package client

import (
	"context"
	"crypto/hmac"
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Runtime-private verified provider output. Configuration is used only on
// success; only node registration/map fields may be copied from it.
type ClientRPCTrustRecoveryResult struct {
	Configuration      *Config
	Failure            *ipc.Failure
	Phase              RecoveryPhase
	RequiresEnrollment bool
}

type ClientRPCTrustRecoveryProvider func(context.Context, Config) (ClientRPCTrustRecoveryResult, error)

func (m *ClientRPCMutations) ReconcileTrustRecovery(ctx context.Context, provider ClientRPCTrustRecoveryProvider) error {
	if provider == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if !lockExitRuntime(ctx, &m.trustWorker) {
		return ctx.Err()
	}
	defer m.trustWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	initial := m.store.Read()
	if initial.RPCState == nil || initial.RPCState.Trust == nil {
		return nil
	}
	plan := *initial.RPCState.Trust
	if !plan.Adopted || !plan.DownStarted || initial.EnrollmentRecovery == nil {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	ready := false
	_, err := m.ReconcileOperation(plan.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if !rpcTrustRecoveryMatches(*cfg, initial, op) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		ready = true
		return nil
	})
	if err != nil || !ready {
		return err
	}
	result, providerErr := provider(ctx, clonePersistentConfig(initial))
	if err := ctx.Err(); err != nil {
		return err
	}
	if providerErr != nil {
		result = ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_INTERNAL, ReasonKey: "trust_recovery_failed"}, Phase: RecoveryPhaseBlocked}
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !rpcTrustRecoveryMatches(*cfg, initial, op) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		profile := cfg.RPCState.Profiles[op.ProfileId]
		if result.Failure != nil {
			if result.Configuration != nil || result.Failure.Code == ipc.ErrorCode_ERROR_CODE_UNSPECIFIED {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if result.RequiresEnrollment {
				if result.Failure.Code != ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT || result.Failure.Retryable {
					return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
				}
				if err := ApplyTerminalRecoveryCleanup(cfg); err != nil {
					return err
				}
				if err := ApplyTerminalRecoveryCleanup(&profile.Configuration); err != nil {
					return err
				}
			} else {
				failed := cfg.EnrollmentRecovery.WithFailure(result.Phase, result.Failure.ReasonKey, result.Failure.ControlRequestId, result.Failure.Retryable, m.now())
				if failed.Validate() != nil {
					return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
				}
				cfg.EnrollmentRecovery = &failed
				saved := failed
				profile.Configuration.EnrollmentRecovery = &saved
			}
			if !result.Failure.Retryable {
				op.State = ipc.OperationState_OPERATION_STATE_FAILED
				op.Outcome = &ipc.Operation_Failure{Failure: result.Failure}
			}
		} else {
			if result.RequiresEnrollment || !rpcValidRecoveredConfig(initial, result.Configuration) {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			copyRPCRecoveryFields(cfg, *result.Configuration)
			copyRPCRecoveryFields(&profile.Configuration, *result.Configuration)
			op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: true}}
		}
		cfg.RPCState.Profiles[profile.ID] = profile
		return nil
	})
	return err
}

func rpcTrustRecoveryMatches(cfg, initial Config, op *ipc.Operation) bool {
	if cfg.RPCState == nil || cfg.RPCState.Trust == nil || initial.RPCState == nil || initial.RPCState.Trust == nil {
		return false
	}
	plan := cfg.RPCState.Trust
	return op.Kind == ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY && op.State == ipc.OperationState_OPERATION_STATE_RUNNING &&
		op.ProfileId == cfg.RPCState.ActiveProfileID && op.Id == plan.OperationID && plan.Adopted && plan.DownStarted &&
		reflect.DeepEqual(plan, initial.RPCState.Trust) && plan.Announced != nil && reflect.DeepEqual(cfg.MapSigningTrust, plan.Announced) &&
		hmac.Equal(plan.Authority, logoutAuthority(cfg)) && reflect.DeepEqual(logoutAuthority(cfg), logoutAuthority(initial)) &&
		cfg.RPCState.Profiles[op.ProfileId].ControlOrigin == plan.ControlOrigin &&
		cfg.EnrollmentRecovery != nil && cfg.EnrollmentRecovery.OperationID == op.Id && cfg.EnrollmentRecovery.Validate() == nil &&
		cfg.EnrollmentRecovery.ConfirmedControlOrigin == plan.ControlOrigin && cfg.EnrollmentRecovery.ConfirmedKeyID == plan.KeyID &&
		reflect.DeepEqual(cfg.EnrollmentRecovery, initial.EnrollmentRecovery) &&
		reflect.DeepEqual(enrollmentFields(cfg), enrollmentFields(initial)) &&
		reflect.DeepEqual(enrollmentFields(cfg.RPCState.Profiles[op.ProfileId].Configuration), enrollmentFields(initial.RPCState.Profiles[op.ProfileId].Configuration)) &&
		reflect.DeepEqual(cfg.RPCState.Profiles[op.ProfileId].Configuration.EnrollmentRecovery, initial.RPCState.Profiles[op.ProfileId].Configuration.EnrollmentRecovery)
}

func rpcValidRecoveredConfig(initial Config, next *Config) bool {
	return next != nil && initial.NodeID != "" && initial.NetworkID != "" && next.NodeCredential != "" && next.NodeID == initial.NodeID && next.NetworkID == initial.NetworkID &&
		next.CachedMap != nil && next.CachedMap.Node.ID == initial.NodeID && next.CachedMap.Network.ID == initial.NetworkID &&
		next.MapRevision >= initial.MapRevision && next.CachedMap.Network.Revision == next.MapRevision && next.CachedMapSavedAt != nil
}

func copyRPCRecoveryFields(dst *Config, src Config) {
	src = clonePersistentConfig(src)
	dst.ResourcePreferences, dst.ResourcePreferencesRetired = src.ResourcePreferences, src.ResourcePreferencesRetired
	dst.NodeID, dst.NetworkID, dst.NodeCredential, dst.NodeApprovalState = src.NodeID, src.NetworkID, src.NodeCredential, src.NodeApprovalState
	dst.MapRevision, dst.MapGlobalRevision, dst.MapHash = src.MapRevision, src.MapGlobalRevision, src.MapHash
	dst.CachedMap, dst.CachedMapSavedAt = src.CachedMap, src.CachedMapSavedAt
	dst.EnrollmentRequestID, dst.EnrollmentPollToken, dst.ApprovalURL = "", "", ""
	dst.EnrollmentRequest, dst.PendingDirectRegistration, dst.EnrollmentRecovery = nil, nil, nil
}
