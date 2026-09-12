package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The connection coordinator supplies its actual phase. Stored enrollment,
// cached-map presence and connected intent do not establish tunnel connectivity.
func observeAgentRPCStatus(ctx context.Context, mutations *client.ClientRPCMutations, opts agentIPCOptions, phase ipc.ConnectionPhase) error {
	return mutations.ObserveStatus(func(cfg client.Config) (*ipc.Status, error) {
		return buildAgentRPCStatus(ctx, opts, cfg, phase), nil
	})
}

func buildAgentRPCStatus(ctx context.Context, opts agentIPCOptions, cfg client.Config, phase ipc.ConnectionPhase) *ipc.Status {
	status := &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, ControlState: ipc.ControlState_CONTROL_STATE_NOT_REGISTERED,
		ConnectionPhase: phase, AccountId: cfg.ActiveAccountID, NodeId: cfg.NodeID, MapRevision: cfg.MapRevision, RouteTable: cfg.WireGuardRouteTable,
		Agent: &ipc.AgentStatus{SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT},
		StoredState: &ipc.StoredStatePresence{CachedMapPresent: cfg.CachedMap != nil, MapSigningTrustPresent: client.HasSigningTrust(cfg),
			TokenPresent: cfg.Token != "", NodeCredentialPresent: cfg.NodeCredential != "", DeviceFingerprintPresent: cfg.DeviceFingerprint != "", IdentityPrivateKeyPresent: cfg.IdentityPrivateKey != "", TunnelPrivateKeyPresent: cfg.PrivateKey != ""}}
	if cfg.RPCState != nil {
		status.ActiveProfileId = cfg.RPCState.ActiveProfileID
	}
	if intent := cfg.ConnectionIntent; intent != nil {
		status.Intent = &ipc.ConnectionIntent{}
		switch intent.DesiredState {
		case client.ConnectionIntentDesiredConnected:
			status.Intent.DesiredState = ipc.DesiredState_DESIRED_STATE_CONNECTED
		case client.ConnectionIntentDesiredDisconnected:
			status.Intent.DesiredState = ipc.DesiredState_DESIRED_STATE_DISCONNECTED
			status.UserDisconnected = true
		}
		if timestamp, err := time.Parse(time.RFC3339Nano, intent.UpdatedAt); err == nil {
			status.Intent.UpdatedAt = timestamppb.New(timestamp)
		}
	}
	if cfg.NodeID != "" {
		status.ServiceState = ipc.ServiceState_SERVICE_STATE_DISCONNECTED
		status.ControlState = ipc.ControlState_CONTROL_STATE_REGISTERED
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		status.ServiceState = ipc.ServiceState_SERVICE_STATE_ERROR
		status.ControlState = ipc.ControlState_CONTROL_STATE_ERROR
		status.Failures = []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, ReasonKey: "local_device_identity_invalid"}}
		return status
	}
	if recovery := cfg.EnrollmentRecovery; recovery != nil {
		status.ServiceState = ipc.ServiceState_SERVICE_STATE_RECOVERING
		status.ControlState = ipc.ControlState_CONTROL_STATE_RECOVERING
		code := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
		switch recovery.Phase {
		case client.RecoveryPhaseBlocked:
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_RECOVERY_BLOCKED
			status.ControlState = ipc.ControlState_CONTROL_STATE_RECOVERY_BLOCKED
		case client.RecoveryPhasePolicyBlocked:
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_POLICY_BLOCKED
			status.ControlState = ipc.ControlState_CONTROL_STATE_POLICY_BLOCKED
			code = ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED
		case client.RecoveryPhaseNeedsLogin:
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_NEEDS_LOGIN
			status.ControlState = ipc.ControlState_CONTROL_STATE_NEEDS_LOGIN
			code = ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN
		}
		status.Recovery = &ipc.Recovery{OperationId: recovery.OperationID, State: status.ServiceState, Failure: &ipc.Failure{Code: code, Retryable: recovery.Retryable, ControlRequestId: recovery.RequestID}}
		return status
	}
	switch strings.ToLower(strings.TrimSpace(cfg.NodeApprovalState)) {
	case clientapi.NodeApprovalPending:
		status.ServiceState = ipc.ServiceState_SERVICE_STATE_NEEDS_APPROVAL
		status.ControlState = ipc.ControlState_CONTROL_STATE_PENDING_APPROVAL
		status.EnrollmentRequestId = cfg.EnrollmentRequestID
		// Browser URLs require a validated provider projection, not raw config.
		status.PendingAction = &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL}
		return status
	case clientapi.NodeApprovalRejected:
		status.ServiceState = ipc.ServiceState_SERVICE_STATE_ERROR
		status.ControlState = ipc.ControlState_CONTROL_STATE_ERROR
		status.Failures = []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED}}
		return status
	}
	if cfg.CachedMap != nil {
		networkMap, err := verifiedCachedNetworkMap(&cfg)
		if err != nil {
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_ERROR
			status.ControlState = ipc.ControlState_CONTROL_STATE_CACHE_INVALID
			status.Failures = []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "cached_map_invalid"}}
			return status
		}
		status.StoredState.CachedMapValid = true
		status.Network = &ipc.Network{Id: networkMap.Network.ID, Name: networkMap.Network.Name, AccountId: networkMap.Network.AccountID, Ipv4Cidr: networkMap.Network.CIDR, Ipv6Cidr: networkMap.Network.IPv6CIDR}
		status.NodeId = networkMap.Node.ID
		status.Hostname = networkMap.Node.Hostname
		status.Ephemeral = networkMap.Node.Ephemeral
		status.MapRevision = networkMap.Network.Revision
		if status.AccountId == "" {
			status.AccountId = networkMap.Network.AccountID
		}
		for _, address := range []string{networkMap.Node.AssignedIP, networkMap.Node.AssignedIPv6} {
			if address != "" {
				status.OverlayAddresses = append(status.OverlayAddresses, address)
			}
		}
		status.PeerCount = uint32(len(networkMap.Peers))
		for _, endpoint := range networkMap.STUNEndpoints {
			status.StunEndpoints = append(status.StunEndpoints, &ipc.Endpoint{Id: endpoint.ID, Address: endpoint.Addr})
		}
		for _, endpoint := range networkMap.Relays {
			status.RelayEndpoints = append(status.RelayEndpoints, &ipc.Endpoint{Id: endpoint.ID, Address: endpoint.Addr, Protocol: endpoint.Protocol, Priority: uint32(max(0, endpoint.Priority))})
		}
		status.ControlState = ipc.ControlState_CONTROL_STATE_OFFLINE_CACHE // A verified cache is not a live probe.
	}
	if status.NodeId != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		status.Control = probeAgentRPCControl(probeCtx, cfg.ControlURLs())
		cancel()
		if status.Control != nil {
			if status.Control.Ok && status.StoredState.CachedMapValid {
				status.ControlState = ipc.ControlState_CONTROL_STATE_READY
			} else if !status.Control.Ok {
				status.ControlState = ipc.ControlState_CONTROL_STATE_DEGRADED
			}
		}
		switch phase {
		case ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED:
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_DEGRADED
			if status.GetControl().GetOk() && status.StoredState.CachedMapValid {
				status.ServiceState = ipc.ServiceState_SERVICE_STATE_CONNECTED
			}
		case ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED:
			status.ServiceState = ipc.ServiceState_SERVICE_STATE_DISCONNECTED
		}
	}
	if snapshot := loadAgentSnapshotIfAvailable(opts.StateOutput); snapshot != nil {
		attachAgentRPCSnapshot(status, *snapshot)
	}
	// Session and credential deadlines remain absent until authoritative providers
	// supply them; neither can be decoded from an unverified bearer token.
	return status
}

func probeAgentRPCControl(ctx context.Context, origins []string) *ipc.ControlProbe {
	var attempts []*ipc.ControlProbe
	client := &http.Client{Transport: http.DefaultTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, origin := range origins {
		probe := &ipc.ControlProbe{}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			probe.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, ReasonKey: "invalid_control_origin"}
		} else {
			probe.Origin = parsed.Scheme + "://" + parsed.Host
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(origin, "/")+"/client/readyz", nil)
			if err == nil {
				response, requestErr := client.Do(request)
				if requestErr == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
					_ = response.Body.Close()
					probe.HttpStatus = uint32(response.StatusCode)
					probe.Ok = response.StatusCode >= 200 && response.StatusCode < 300
				}
			}
			if !probe.Ok {
				probe.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "control_probe_failed", Retryable: true}
			}
		}
		attempts = append(attempts, probe)
		if probe.Ok {
			break
		}
	}
	if len(attempts) == 0 {
		return nil
	}
	last := attempts[len(attempts)-1]
	result := &ipc.ControlProbe{Ok: last.Ok, Origin: last.Origin, HttpStatus: last.HttpStatus, Failure: last.Failure}
	if len(attempts) > 1 {
		result.Attempts = attempts
	}
	return result
}

func attachAgentRPCSnapshot(status *ipc.Status, snapshot client.AgentSnapshot) {
	if status.Network == nil || snapshot.NodeID != status.NodeId || snapshot.NetworkID != status.Network.Id || snapshot.MapRevision > status.MapRevision {
		return
	}
	state := ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT
	if snapshot.MapRevision < status.MapRevision {
		state = ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
	}
	agent := &ipc.AgentStatus{SnapshotState: state, MapRevision: snapshot.MapRevision, NodeId: snapshot.NodeID, NetworkId: snapshot.NetworkID, PeerCount: uint32(max(0, snapshot.PeerCount)), StunOk: snapshot.STUN.OK, RelayOk: snapshot.Relay.OK, RelayAttemptCount: uint32(len(snapshot.Relay.Attempts))}
	if state == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS {
		agent.TargetMapRevision = status.MapRevision
	}
	if timestamp, err := time.Parse(time.RFC3339Nano, snapshot.GeneratedAt); err == nil {
		agent.GeneratedAt = timestamppb.New(timestamp)
	}
	for _, address := range []string{snapshot.OverlayIP, snapshot.OverlayIPv6} {
		if address != "" {
			agent.OverlayAddresses = append(agent.OverlayAddresses, address)
		}
	}
	if selected := snapshot.Relay.Selected; selected != nil {
		agent.SelectedRelay = &ipc.Endpoint{Id: selected.ID, Address: selected.Addr, Protocol: selected.Protocol, Priority: uint32(max(0, selected.Priority))}
	}
	for _, path := range snapshot.Paths {
		switch path.SelectedPath {
		case "direct":
			agent.DirectPathCount++
		case "relay":
			agent.RelayPathCount++
		}
	}
	if snapshot.LastError != "" {
		agent.LastFailure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "agent_observation_failed"}
	}
	status.Agent = agent
}
