package localapi

import (
	"context"
	"errors"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// connectionDetails recognizes only finite safe failures; arbitrary backend
// text, native codes and transport bodies are never projected into the API.
func connectionDetails(err error) *clientv0.Failure {
	var kind connectivity.FailureKind
	if !errors.As(err, &kind) {
		if errors.Is(err, connectivity.ErrClock) {
			return &clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "client.connection.clock_unsafe", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR}
		}
		return nil
	}
	f := &clientv0.Failure{Retryable: true, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	switch kind {
	case connectivity.FailureAccess:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.access_denied", clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR, false
	case connectivity.FailurePermission:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, "client.connection.permission_required", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR, false
	case connectivity.FailureConflict:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT, "client.connection.resource_conflict", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR, false
	case connectivity.FailureApply:
		f.Code, f.ReasonKey, f.ActionOwner = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.apply_failed", clientv0.ActionOwner_ACTION_OWNER_SUPPORT
	case connectivity.FailurePath:
		f.Code, f.ReasonKey = clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, "client.connection.path_unconfirmed"
	case connectivity.FailureCleanup:
		f.Code, f.ReasonKey, f.ActionOwner = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.cleanup_unconfirmed", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
	case connectivity.FailureControl:
		f.Code, f.ReasonKey = clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, "client.connection.control_unavailable"
	case connectivity.FailureDNSAssignment:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.dns_assignment_invalid", clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR, false
	case connectivity.FailureDNSUnsupported:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.dns_unsupported", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR, false
	case connectivity.FailureMap:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.map_rejected", clientv0.ActionOwner_ACTION_OWNER_SUPPORT, false
	case connectivity.FailureConfiguration:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.configuration_invalid", clientv0.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR, false
	case connectivity.FailureCheckpoint:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.checkpoint_unavailable", clientv0.ActionOwner_ACTION_OWNER_SUPPORT, false
	case connectivity.FailureProtocol:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED, "client.connection.protocol_rejected", clientv0.ActionOwner_ACTION_OWNER_SUPPORT, false
	case connectivity.FailureTrust:
		f.Code, f.ReasonKey, f.Retryable = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.trust_required", false
	case connectivity.FailureCredentialInvalid:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.credential_rejected", clientv0.ActionOwner_ACTION_OWNER_SUPPORT, false
	case connectivity.FailureCredential:
		f.Code, f.ReasonKey, f.Retryable = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.credential_expired", false
	case connectivity.FailureLogin:
		f.Code, f.ReasonKey, f.Retryable = clientv0.ErrorCode_ERROR_CODE_NEEDS_LOGIN, "client.connection.login_required", false
	case connectivity.FailureNodeRenewal:
		f.Code, f.ReasonKey, f.Retryable, f.ActionOwner = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.node_renewal_unresolved", false, clientv0.ActionOwner_ACTION_OWNER_SUPPORT
	case connectivity.FailureRevoked:
		f.Code, f.ReasonKey, f.ActionOwner, f.Retryable = clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED, "client.connection.registration_revoked", clientv0.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR, false
	default:
		return nil
	}
	return f
}

func observedConnectionFailure(err error) *clientv0.Failure {
	if err == nil {
		return nil
	}
	if f := connectionDetails(err); f != nil {
		return f
	}
	f := &clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "client.connection.failed", Retryable: true, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		f.Code, f.ReasonKey = clientv0.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, "client.connection.deadline"
	case errors.Is(err, context.Canceled):
		f.Code, f.ReasonKey = clientv0.ErrorCode_ERROR_CODE_CANCELLED, "client.connection.cancelled"
	case errors.Is(err, state.ErrStale):
		f.Code, f.ReasonKey = clientv0.ErrorCode_ERROR_CODE_STALE_STATE, "client.connection.stale"
	}
	return f
}

func observedMapMatchesTarget(local connectivity.LocalObservation, target *state.MapCursor) bool {
	return target == nil || local.MapRevision == target.Revision.Network && local.MapPayloadHash == target.PayloadHash
}

func projectConnection(r *clientv0.Status, p state.Profile, v connectivity.Observation) {
	connection := p.Connection
	if connection == nil {
		return
	}
	desired := clientv0.DesiredState_DESIRED_STATE_DISCONNECTED
	if connection.Intent == state.IntentConnected {
		desired = clientv0.DesiredState_DESIRED_STATE_CONNECTED
	}
	r.Intent = &clientv0.ConnectionIntent{DesiredState: desired, UpdatedAt: timestamppb.New(connection.AcceptedAt)}
	r.UserDisconnected = connection.Intent == state.IntentDisconnected
	r.ServiceState = clientv0.ServiceState_SERVICE_STATE_RECOVERING
	r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED
	if v.Profile == "" {
		return
	}
	if f := observedConnectionFailure(v.Failure); f != nil {
		r.Failures = append(r.Failures, f)
	}
	if f := observedConnectionFailure(v.CleanupFailure); f != nil {
		r.Failures = append(r.Failures, f)
	}
	if v.Phase == connectivity.PhaseConnected && v.Local.Permission.ClockUnsafe {
		r.ServiceState = clientv0.ServiceState_SERVICE_STATE_ERROR
		r.ControlState = clientv0.ControlState_CONTROL_STATE_CACHE_INVALID
		r.Failures = append(r.Failures, connectionDetails(connectivity.ErrClock))
		return
	}
	if v.Phase == connectivity.PhaseConnected && v.Local.Permission.Revoked {
		r.ServiceState = clientv0.ServiceState_SERVICE_STATE_POLICY_BLOCKED
		r.ControlState = clientv0.ControlState_CONTROL_STATE_POLICY_BLOCKED
		r.Failures = append(r.Failures, connectionDetails(connectivity.FailureAccess))
		return
	}
	matchesTarget := observedMapMatchesTarget(v.Local, connection.MapCursor)
	if (!v.Local.Known || !v.Local.Applied) && connection.MapCursor != nil && connection.Intent == state.IntentConnected && p.Attempt != nil {
		r.Agent = &clientv0.AgentStatus{SnapshotState: clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT, NodeId: r.NodeId, NetworkId: p.Attempt.NetworkID, TargetMapRevision: connection.MapCursor.Revision.Network}
		if last := connection.LastAppliedMap; last != nil {
			r.Agent.SnapshotState = clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
			r.Agent.MapRevision = last.Revision.Network
		}
	}
	if v.Local.Known && v.Local.Applied {
		local := v.Local
		r.MapRevision, r.PeerCount = local.MapRevision, local.PeerCount
		if local.Address != "" {
			r.OverlayAddresses = []string{local.Address}
		}
		r.Agent = &clientv0.AgentStatus{SnapshotState: clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS, MapRevision: local.MapRevision, ConnectionPaused: !local.Permission.NewFlows, NodeId: r.NodeId, NetworkId: p.Attempt.NetworkID, OverlayAddresses: append([]string(nil), r.OverlayAddresses...), PeerCount: local.PeerCount, GeneratedAt: timestamppb.New(local.GeneratedAt), DirectPathCount: local.DirectPaths}
		r.Agent.RelayPathCount = local.RelayPaths
		for _, transport := range local.PeerTransports {
			if transport != nil {
				r.PeerTransports = append(r.PeerTransports, proto.Clone(transport).(*clientv0.PeerTransportStatus))
			}
		}
		if local.Permission.NewFlows && matchesTarget {
			r.Agent.SnapshotState = clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT
		}
		if connection.MapCursor != nil {
			r.Agent.TargetMapRevision = connection.MapCursor.Revision.Network
		}
	}
	switch v.Phase {
	case connectivity.PhaseConnecting:
		r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING
	case connectivity.PhaseDisconnecting:
		r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTING
	case connectivity.PhaseDisconnected:
		if !v.CleanupPending && connection.Journal == nil {
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_DISCONNECTED
			r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
			if pending := connection.EndpointRequest; pending != nil && pending.IntentGeneration == connection.Generation && pending.Receipt == nil {
				r.ControlState = clientv0.ControlState_CONTROL_STATE_DEGRADED
				r.Failures = append(r.Failures, &clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "client.connection.discovery_unconfirmed", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT})
			}
		}
	case connectivity.PhaseConnected:
		local := v.Local
		if local.ControlUnavailable {
			r.Failures = append(r.Failures, connectionDetails(connectivity.FailureControl))
		}
		if local.EndpointRefreshUnconfirmed {
			r.Failures = append(r.Failures, &clientv0.Failure{
				Code: clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "client.connection.endpoint_refresh_unconfirmed",
				Retryable: true, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_SUPPORT,
			})
		}
		if !local.Known || !local.Applied {
			return
		}
		r.ServiceState = clientv0.ServiceState_SERVICE_STATE_DEGRADED
		r.ControlState = clientv0.ControlState_CONTROL_STATE_DEGRADED
		if local.Permission.ExistingFlows {
			r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED
			if !local.Permission.NewFlows {
				r.ControlState = clientv0.ControlState_CONTROL_STATE_OFFLINE_CACHE
			}
		}
		if matchesTarget && local.Permission.NewFlows && local.PeerCount > 0 && local.MapRevision > 0 && local.Address != "" {
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_CONNECTED
			r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED
			r.ControlState = clientv0.ControlState_CONTROL_STATE_READY
		}
		if matchesTarget && local.Permission.NewFlows && local.SoleNodeReady && local.PeerCount == 0 && local.DirectPaths == 0 && local.RelayPaths == 0 && local.MapRevision > 0 && local.Address != "" {
			r.ServiceState = clientv0.ServiceState_SERVICE_STATE_LOCAL_READY
			r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_LOCAL_READY
			r.ControlState = clientv0.ControlState_CONTROL_STATE_READY
		}
		if local.EndpointRefreshUnconfirmed && r.ControlState == clientv0.ControlState_CONTROL_STATE_READY {
			r.ControlState = clientv0.ControlState_CONTROL_STATE_DEGRADED
		}
	case connectivity.PhaseFailed:
		if v.RetryScheduled {
			r.Failures = append(r.Failures, &clientv0.Failure{
				Code: clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "client.connection.retry_scheduled",
				Retryable: true, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER,
			})
		}
		r.ServiceState = clientv0.ServiceState_SERVICE_STATE_ERROR
		if !v.CleanupPending && connection.Journal == nil {
			r.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
		}
	}
}
