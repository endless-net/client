package clientapp

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
)

// nativeConnectionFailure discards raw backend diagnostics. Keep only known
// actionable classifications, with no arbitrary message or native code payload.
func nativeConnectionFailure(err error, fallback connectivity.FailureKind) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, control.ErrNodeRenewalUnresolved):
		return connectivity.FailureNodeRenewal
	case errors.Is(err, control.ErrCredentialsLogin):
		return connectivity.FailureLogin
	case errors.Is(err, control.ErrBootstrapUnavailable), errors.Is(err, connectivity.ErrUnavailable):
		return connectivity.FailureControl
	case errors.Is(err, control.ErrBootstrapState):
		return connectivity.FailureCheckpoint
	case errors.Is(err, control.ErrBootstrapProtocol), errors.Is(err, connectivity.ErrProtocol):
		return connectivity.FailureProtocol
	case errors.Is(err, connectivity.ErrDNSAssignment):
		return connectivity.FailureDNSAssignment
	case errors.Is(err, connectivity.ErrDNSUnsupported):
		return connectivity.FailureDNSUnsupported
	case errors.Is(err, connectivity.ErrMapRevisionRollback):
		return connectivity.FailureMapRevisionRollback
	case errors.Is(err, connectivity.ErrMapIssueRollback):
		return connectivity.FailureMapIssueRollback
	case errors.Is(err, connectivity.ErrMapContentConflict):
		return connectivity.FailureMapContentConflict
	case errors.Is(err, connectivity.ErrControlConflict):
		return connectivity.FailureControlConflict
	case errors.Is(err, connectivity.ErrMap), errors.Is(err, connectivity.ErrStale):
		return connectivity.FailureMap
	case errors.Is(err, connectivity.ErrConfig):
		return connectivity.FailureConfiguration
	case errors.Is(err, control.ErrBootstrap), errors.Is(err, control.ErrCredentialsProtocol):
		return connectivity.FailureTrust
	case errors.Is(err, dataplane.ErrAdapterPermission), errors.Is(err, dataplane.ErrIPPermission), errors.Is(err, dataplane.ErrPolicyPermission):
		return connectivity.FailurePermission
	case errors.Is(err, dataplane.ErrAdapterConflict), errors.Is(err, dataplane.ErrIPConflict), errors.Is(err, dataplane.ErrAdapterOwnership), errors.Is(err, dataplane.ErrIPOwnership), errors.Is(err, dataplane.ErrPolicyOwnership):
		return connectivity.FailureConflict
	case errors.Is(err, connectivity.ErrClock):
		return connectivity.ErrClock
	case errors.Is(err, connectivity.ErrTrust):
		return connectivity.FailureTrust
	case errors.Is(err, connectivity.ErrCredentialInvalid):
		return connectivity.FailureCredentialInvalid
	case errors.Is(err, connectivity.ErrCredentialExpired):
		return connectivity.FailureCredential
	case errors.Is(err, connectivity.ErrRevoked):
		return connectivity.FailureRevoked
	case errors.Is(err, control.ErrAccountNodeDenied), errors.Is(err, connectivity.ErrDenied), errors.Is(err, connectivity.ErrExpired):
		return connectivity.FailureAccess
	case errors.Is(err, state.ErrStale):
		return state.ErrStale
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		if fallback == connectivity.FailurePath {
			return connectivity.FailurePath
		}
		return context.DeadlineExceeded
	default:
		return fallback
	}
}

// connectionFailureCode returns an allowlisted diagnostic code shared by both runtimes.
func connectionFailureCode(err error) string {
	switch {
	case errors.Is(err, connectivity.ErrMapRevisionRollback):
		return "map_revision_rollback"
	case errors.Is(err, connectivity.ErrMapIssueRollback):
		return "map_issue_time_rollback"
	case errors.Is(err, connectivity.ErrMapContentConflict):
		return "map_content_conflict"
	case errors.Is(err, connectivity.ErrControlConflict):
		return "control_conflict"
	case errors.Is(err, connectivity.ErrStale):
		return "map_stale"
	case errors.Is(err, connectivity.ErrMapEvent):
		return "map_event_invalid"
	case errors.Is(err, connectivity.ErrMapNotYetValid):
		return "map_issue_time_future"
	case errors.Is(err, connectivity.ErrMapAlreadyExpired):
		return "map_expired_at_verification"
	case errors.Is(err, connectivity.ErrMapCredentialBound):
		return "map_exceeds_credential"
	case errors.Is(err, connectivity.ErrMapTTL):
		return "map_ttl_exceeded"
	case errors.Is(err, connectivity.ErrMapEnvelope):
		return "map_envelope_invalid"
	case errors.Is(err, connectivity.ErrMapRelay):
		return "map_relay_invalid"
	case errors.Is(err, connectivity.ErrMapProfile):
		return "map_profile_invalid"
	case errors.Is(err, connectivity.ErrEndpointUncertain):
		return "endpoint_outcome_unknown"
	case errors.Is(err, dataplane.ErrTransportCapacity):
		return "endpoint_capacity"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	}
	switch nativeConnectionFailure(err, connectivity.FailureUnknown) {
	case connectivity.FailureControl:
		return "unavailable"
	case connectivity.FailureCheckpoint:
		return "checkpoint_unavailable"
	case connectivity.FailureProtocol:
		return "protocol_rejected"
	case connectivity.FailureCredentialInvalid:
		return "credential_rejected"
	case connectivity.FailureDNSAssignment:
		return "dns_assignment_invalid"
	case connectivity.FailureDNSUnsupported:
		return "dns_unsupported"
	case connectivity.FailureMap:
		return "map_rejected"
	case connectivity.FailureTrust:
		return "trust_required"
	case connectivity.FailureConfiguration:
		return "invalid_configuration"
	default:
		return "failed"
	}
}

func mapFailureFields(fields []any, err error) []any {
	var d *connectivity.MapStreamFailure
	if !errors.As(err, &d) {
		return fields
	}
	fields = append(fields, "rpc", "WatchMaps", "map_step", d.Step, "full_snapshot", d.FullSnapshot, "requested_revision", d.RequestRevision, "received_records", d.Records, "last_event_revision", d.EventRevision)
	if d.RPCCode != "" {
		fields = append(fields, "rpc_code", d.RPCCode)
	}
	return fields
}
