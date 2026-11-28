package localapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type observedConnection struct {
	value connectivity.Observation
	calls int
}

func TestRegistrationFailureInOwnerStatus(t *testing.T) {
	c, _, _, now := fixture(t)
	owner := localipc.Principal{SID: "owner"}
	p := state.Profile{ID: defaultID, Attempt: &enrollment.Attempt{Phase: enrollment.Registering, Uncertain: true, RecoveryUntil: now.Add(time.Minute)}}
	s := state.State{OwnerSID: owner.SID, ActiveProfileID: p.ID, Profiles: map[string]state.Profile{}}
	for _, reason := range []string{"client.registration.request_rejected", "client.registration.access_denied", "private backend detail"} {
		p.Registration.BlockedReason = reason
		s.Profiles[p.ID] = p
		view, _ := c.statusAndObservation(s, owner)
		if reason == "private backend detail" {
			if len(view.Failures) != 0 {
				t.Fatal("arbitrary reason exposed")
			}
		} else if len(view.Failures) != 1 || view.Failures[0].ReasonKey != reason || view.Failures[0].Retryable || view.ControlState != clientv0.ControlState_CONTROL_STATE_RECOVERING {
			t.Fatal("registration outcome and rejection must remain distinct", view.Failures)
		}
		foreign, _ := c.statusAndObservation(s, localipc.Principal{SID: "other"})
		if len(foreign.Failures) != 0 {
			t.Fatal("foreign caller received registration failure")
		}
	}
}

func TestRecoveryFailuresRemainDistinctAndSafe(t *testing.T) {
	for kind, reason := range map[connectivity.FailureKind]string{
		connectivity.FailureTrust:             "client.connection.trust_required",
		connectivity.FailureCredential:        "client.connection.credential_expired",
		connectivity.FailureNodeRenewal:       "client.connection.node_renewal_unresolved",
		connectivity.FailureAccess:            "client.connection.access_denied",
		connectivity.FailureRevoked:           "client.connection.registration_revoked",
		connectivity.FailurePath:              "client.connection.path_unconfirmed",
		connectivity.FailureCleanup:           "client.connection.cleanup_unconfirmed",
		connectivity.FailureLogin:             "client.connection.login_required",
		connectivity.FailureDNSAssignment:     "client.connection.dns_assignment_invalid",
		connectivity.FailureDNSUnsupported:    "client.connection.dns_unsupported",
		connectivity.FailureMap:               "client.connection.map_rejected",
		connectivity.FailureConfiguration:     "client.connection.configuration_invalid",
		connectivity.FailureCheckpoint:        "client.connection.checkpoint_unavailable",
		connectivity.FailureProtocol:          "client.connection.protocol_rejected",
		connectivity.FailureCredentialInvalid: "client.connection.credential_rejected",
	} {
		f := observedConnectionFailure(kind)
		if f == nil || f.ReasonKey != reason {
			t.Fatal("unsafe or conflated recovery reason", kind, f)
		}
		switch kind {
		case connectivity.FailureTrust, connectivity.FailureCredential, connectivity.FailureMap,
			connectivity.FailureDNSAssignment, connectivity.FailureDNSUnsupported,
			connectivity.FailureConfiguration, connectivity.FailureCheckpoint, connectivity.FailureProtocol,
			connectivity.FailureCredentialInvalid:
			if f.Retryable {
				t.Fatal("terminal condition advertised as retryable")
			}
		}
	}
}

func (o *observedConnection) ConnectionSnapshot() connectivity.Observation {
	o.calls++
	return o.value
}

func TestConnectionStatusRequiresCurrentGenerationAndAppliedTunnel(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	online := connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{NewFlows: true, ExistingFlows: true}, DirectPaths: 1, MapRevision: 8, Address: "100.64.0.7", PeerCount: 1}
	online.PeerTransports = []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_DIRECT, Engine: "amneziawg", ProtocolVersion: "3.1", TransportProfileId: "profile", TransportProfileRevision: 7, SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_IDLE, ReasonKey: "client.transport.direct"}}
	sole := connectivity.LocalObservation{Known: true, Applied: true, SoleNodeReady: true, Permission: online.Permission, MapRevision: 8, Address: "100.64.0.7"}
	soleOffline := sole
	soleOffline.Permission = connectivity.Permission{ExistingFlows: true}
	soleOffline.ControlUnavailable = true
	soleWithPeer := sole
	soleWithPeer.PeerCount = 1
	soleWithPeer.SoleNodeReady = false
	soleWithPeer.PeerTransports = []*clientv0.PeerTransportStatus{{PeerId: "peer"}}
	relay := online
	relay.DirectPaths, relay.RelayPaths = 0, 1
	unsupported := relay
	unsupported.PeerTransports = []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_RELAY, Engine: "wireguard", ProtocolVersion: "1", SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_UNSUPPORTED, ReasonKey: "client.transport.capabilities_incompatible"}}
	for _, tc := range []struct {
		name      string
		phase     connectivity.Phase
		local     connectivity.LocalObservation
		stale     bool
		want      clientv0.ServiceState
		wantPhase clientv0.ConnectionPhase
	}{
		{"pending", connectivity.PhaseConnecting, connectivity.LocalObservation{}, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING},
		{"verified_sole_node", connectivity.PhaseConnected, sole, false, clientv0.ServiceState_SERVICE_STATE_LOCAL_READY, clientv0.ConnectionPhase_CONNECTION_PHASE_LOCAL_READY},
		{"stale_sole_node", connectivity.PhaseConnected, sole, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"offline_sole_node", connectivity.PhaseConnected, soleOffline, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"peer_without_authenticated_path", connectivity.PhaseConnected, soleWithPeer, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"before_endpoint_ack", connectivity.PhaseConnecting, soleWithPeer, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING},
		{"applied_path", connectivity.PhaseConnected, online, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"applied_relay", connectivity.PhaseConnected, relay, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"relay_with_unsupported_direct", connectivity.PhaseConnected, unsupported, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"unsupported_stale_generation", connectivity.PhaseConnected, unsupported, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"busy_unknown", connectivity.PhaseConnected, connectivity.LocalObservation{}, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"stale_generation", connectivity.PhaseConnected, online, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"listener_without_path", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true, Permission: online.Permission}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"offline_existing", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{ExistingFlows: true}}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"source_blocked", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"unsafe_clock", connectivity.PhaseConnected, connectivity.LocalObservation{Permission: connectivity.Permission{ClockUnsafe: true}}, false, clientv0.ServiceState_SERVICE_STATE_ERROR, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"revoked", connectivity.PhaseConnected, connectivity.LocalObservation{Permission: connectivity.Permission{Revoked: true}}, false, clientv0.ServiceState_SERVICE_STATE_POLICY_BLOCKED, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generation := uint64(1)
			if tc.stale {
				generation++
			}
			o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: generation, Intent: state.IntentConnected, Phase: tc.phase, Local: tc.local}}
			c.config.ConnectionObservations = o
			got, err := c.snapshot(context.Background(), caller)
			if err != nil || got.Status.ServiceState != tc.want || got.Status.ConnectionPhase != tc.wantPhase || got.Status.Intent.DesiredState != clientv0.DesiredState_DESIRED_STATE_CONNECTED {
				t.Fatal("intent, application and path conflated", got, err)
			}
			if tc.local.ControlUnavailable && (len(got.Status.Failures) != 1 || got.Status.Failures[0].ReasonKey != "client.connection.control_unavailable" || !got.Status.Failures[0].Retryable) {
				t.Fatal("offline retry was not reported as temporary control failure", got.Status.Failures)
			}
			if tc.stale && (got.Status.MapRevision != 0 || got.Status.Agent != nil || len(got.Status.PeerTransports) != 0) {
				t.Fatal("stale backend generation leaked into current snapshot")
			}
			if !tc.stale && tc.local.Known && tc.local.Applied && (got.Status.Agent == nil || got.Status.Agent.RelayPathCount != tc.local.RelayPaths || got.Status.Agent.DirectPathCount != tc.local.DirectPaths) {
				t.Fatal("measured path counts were lost in the producer IPC projection")
			}
			if got.Status.ControlState == clientv0.ControlState_CONTROL_STATE_READY && got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED && got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_LOCAL_READY {
				t.Fatal("unconfirmed path claimed ready control")
			}
			if !tc.stale && tc.local.Known && tc.local.Applied && len(tc.local.PeerTransports) > 0 {
				expected := tc.local.PeerTransports[0]
				if len(got.Status.PeerTransports) != 1 || got.Status.PeerTransports[0].TransportProfileRevision != expected.TransportProfileRevision || got.Status.PeerTransports[0].SearchState != expected.SearchState || got.Status.PeerTransports[0].SelectedPath != expected.SelectedPath || got.Status.PeerTransports[0].ReasonKey != expected.ReasonKey {
					t.Fatal("public profile observation lost", got)
				}
				engine := expected.Engine
				got.Status.PeerTransports[0].Engine = "reader mutation"
				if tc.local.PeerTransports[0].Engine != engine {
					t.Fatal("IPC response aliased runtime snapshot")
				}
			}
		})
	}
}

func TestConnectionStatusSeparatesFailureFromCleanupAndFiltersObserver(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseFailed, Failure: connectivity.FailurePermission, CleanupPending: true, CleanupFailure: connectivity.FailureCleanup}}
	c.config.ConnectionObservations = o
	got, err := c.snapshot(context.Background(), caller)
	if err != nil || got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_ERROR || got.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || len(got.Status.Failures) != 2 || got.Status.Failures[0].Code != clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED || got.Status.Failures[1].ReasonKey != "client.connection.cleanup_unconfirmed" {
		t.Fatal("permission or uncertain cleanup erased", got, err)
	}
	o.value.CleanupPending, o.value.CleanupFailure = false, nil
	got, err = c.snapshot(context.Background(), caller)
	if err != nil || got.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || len(got.Status.Failures) != 1 {
		t.Fatal("confirmed safe cleanup lost primary failure", got, err)
	}
	o.value.Failure = errors.New("synthetic-secret backend response")
	got, err = c.snapshot(context.Background(), caller)
	if err != nil || strings.Contains(protojson.Format(got), "synthetic-secret") || got.Status.Failures[0].ReasonKey != "client.connection.failed" {
		t.Fatal("raw failure leaked", err)
	}
	calls := o.calls
	got, err = c.snapshot(context.Background(), localipc.Principal{SID: "another-user"})
	if err != nil || o.calls != calls || got.Status.ActiveProfileId != "" || got.Status.Agent != nil || len(got.Status.Failures) != 0 {
		t.Fatal("non-owner inspected private connection", err)
	}
}

func TestTypedConnectionRPCFailuresNeverExposeBackendText(t *testing.T) {
	for _, tc := range []struct {
		kind  connectivity.FailureKind
		code  codes.Code
		typed clientv0.ErrorCode
	}{
		{connectivity.FailurePermission, codes.PermissionDenied, clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED},
		{connectivity.FailureAccess, codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED},
		{connectivity.FailureConflict, codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT},
		{connectivity.FailureApply, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED},
		{connectivity.FailurePath, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{connectivity.FailureCleanup, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED},
		{connectivity.FailureControl, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE},
	} {
		err := rpcError(errors.Join(tc.kind, errors.New("synthetic-secret native path")))
		if status.Code(err) != tc.code || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("classification lost or text leaked", err)
		}
		details := status.Convert(err).Details()
		if len(details) != 1 || details[0].(*clientv0.Failure).Code != tc.typed {
			t.Fatal("typed detail lost", details)
		}
	}
}

func TestEndpointRefreshDegradesControlWhileTunnelRemainsApplied(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(t.Context(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	local := connectivity.LocalObservation{
		Known: true, Applied: true, MapRevision: 8, Address: "100.64.0.7", PeerCount: 1, DirectPaths: 1,
		Permission:                 connectivity.Permission{NewFlows: true, ExistingFlows: true},
		EndpointRefreshUnconfirmed: true,
	}
	o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseConnected, Local: local}}
	c.config.ConnectionObservations = o
	got, err := c.snapshot(t.Context(), caller)
	if err != nil || got.Status.ControlState != clientv0.ControlState_CONTROL_STATE_DEGRADED ||
		got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED ||
		len(got.Status.Failures) != 1 || got.Status.Failures[0].ReasonKey != "client.connection.endpoint_refresh_unconfirmed" {
		t.Fatal("endpoint failure hidden or tunnel incorrectly demoted", err)
	}
	o.value.Local.EndpointRefreshUnconfirmed = false
	got, err = c.snapshot(t.Context(), caller)
	if err != nil || got.Status.ControlState != clientv0.ControlState_CONTROL_STATE_READY || len(got.Status.Failures) != 0 {
		t.Fatal("confirmed discovery did not restore the ready control status", err)
	}
}

func TestStatusAndPeersDistinguishAppliedMapFromTarget(t *testing.T) {
	c, _, _, now, caller := connectionFixture(t)
	ctx := t.Context()
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	c.config.LogPageKey = [32]byte{1}
	c.config.LogResponseBytes = 65536
	target := state.MapCursor{Revision: coordinator.MapRevision{Network: 9}, PayloadHash: strings.Repeat("a", 64), IssuedAt: *now}
	value, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(ctx, value.Revision, func(s *state.State) error { s.Profiles[defaultID].Connection.MapCursor = &target; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		rev   uint64
		hash  string
		state clientv0.AgentSnapshotState
	}{
		{"first installation failed", 0, "", clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT},
		{"older revision", 8, strings.Repeat("b", 64), clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS},
		{"different signed map at same revision", 9, strings.Repeat("b", 64), clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS},
		{"applied target", 9, target.PayloadHash, clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseFailed, Failure: connectivity.FailureApply}
			if tc.rev != 0 {
				v.Phase, v.Failure = connectivity.PhaseConnected, nil
				v.Local = connectivity.LocalObservation{Known: true, Applied: true, MapRevision: tc.rev, MapPayloadHash: tc.hash, Address: "100.64.0.2", Permission: connectivity.Permission{NewFlows: true, ExistingFlows: true}, PeerCount: 1, Peers: []*clientv0.Peer{{Id: "peer"}}}
			}
			c.config.ConnectionObservations = &observedConnection{value: v}
			got, err := c.snapshot(ctx, caller)
			if err != nil {
				t.Fatal(err)
			}
			meta := got.Status.Agent
			if meta.GetTargetMapRevision() != 9 || meta.GetMapRevision() != tc.rev || meta.GetSnapshotState() != tc.state {
				t.Fatal("status", meta)
			}
			current := tc.state == clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT
			if (got.Status.ControlState == clientv0.ControlState_CONTROL_STATE_READY) != current {
				t.Fatal("control status", got.Status.ControlState)
			}
			if tc.rev == 0 && (got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_ERROR || len(got.Status.Failures) == 0) {
				t.Fatal("missing OS apply failure")
			}
			peers, err := c.listPeers(ctx, caller, &clientv0.ListPeersRequest{})
			if err != nil || peers.GetTargetMapRevision() != 9 || peers.GetSnapshotState() != tc.state || peers.GetMapRevision() != tc.rev || (len(peers.GetPeers()) != 0) != current {
				t.Fatal("peers", peers, err)
			}
		})
	}
	// A successful application was recorded, then a later target failed after
	// the native session had already dropped its in-memory observation.
	v, err := c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(ctx, v.Revision, func(s *state.State) error {
		applied := target
		s.Profiles[defaultID].Connection.LastAppliedMap = &applied
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v, err = c.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(ctx, v.Revision, func(s *state.State) error {
		newer := target
		newer.Revision.Network++
		newer.PayloadHash = strings.Repeat("c", 64)
		newer.IssuedAt = newer.IssuedAt.Add(time.Second)
		s.Profiles[defaultID].Connection.MapCursor = &newer
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c.config.ConnectionObservations = &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseFailed, Failure: connectivity.FailureApply}}
	got, err := c.snapshot(ctx, caller)
	if err != nil {
		t.Fatal(err)
	}
	agent := got.Status.Agent
	if agent.GetMapRevision() != 9 || agent.GetTargetMapRevision() != 10 || agent.GetSnapshotState() != clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS || got.Status.MapRevision != 0 || len(got.Status.OverlayAddresses) != 0 {
		t.Fatal("failure lost history or claimed live authority", got.Status)
	}
	rows, err := c.listPeers(ctx, caller, &clientv0.ListPeersRequest{})
	if err != nil || rows.GetMapRevision() != 9 || rows.GetTargetMapRevision() != 10 || rows.GetSnapshotState() != agent.GetSnapshotState() || len(rows.GetPeers()) != 0 {
		t.Fatal(rows, err)
	}
}
