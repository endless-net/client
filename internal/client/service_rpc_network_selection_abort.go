package client

import (
	"context"
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileNetworkSelectionAbort compensates only a not-yet-activated target.
// The private target is retained until remote cleanup and any uncertain local
// teardown are confirmed. Runtime shutdown leaves the same plan recoverable.
func (m *ClientRPCMutations) ReconcileNetworkSelectionAbort(ctx context.Context, driver ClientRPCProfileDriver, provider ClientRPCNetworkTargetCleanupProvider, code ipc.ErrorCode) error {
	switch code {
	case ipc.ErrorCode_ERROR_CODE_CANCELLED, ipc.ErrorCode_ERROR_CODE_STALE_STATE, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED,
		ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, ipc.ErrorCode_ERROR_CODE_NOT_FOUND, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED:
	default:
		return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.networkSelectionWorker.Lock()
	defer m.networkSelectionWorker.Unlock()
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
	if plan.Activated || plan.ApplyStarted {
		return stale()
	}
	if plan.AbortFailure == nil {
		_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.ProfileId != plan.Profile.ID || !reflect.DeepEqual(current.RPCState.NetworkSelection, plan) {
				return stale()
			}
			current.RPCState.NetworkSelection.AbortFailure = &ipc.Failure{Code: code, ReasonKey: "network_selection_aborted"}
			op.State = ipc.OperationState_OPERATION_STATE_RUNNING
			op.UserAction = nil
			return nil
		})
		if err != nil {
			return err
		}
		cfg = m.store.Read()
		plan = cfg.RPCState.NetworkSelection
	}
	if !plan.TargetRevoked {
		if plan.Target != nil {
			if provider == nil {
				return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			if !networkSelectionCleanupTargetIsolated(cfg, plan, *plan.Target) {
				return stale()
			}
			save := m.NetworkSelectionSaveCallback(ctx, plan.OperationID, cfg)
			var checkpointErr error
			err := provider(ctx, clonePersistentConfig(*plan.Target), ClientRPCNetworkRegistrationInput{OperationID: plan.OperationID, NetworkID: plan.NetworkID}, func(next Config) error {
				if checkpointErr != nil {
					return checkpointErr
				}
				checkpointErr = save(next)
				if checkpointErr == nil {
					copy := clonePersistentConfig(next)
					plan.Target = &copy
				}
				return checkpointErr
			})
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if checkpointErr != nil {
				return checkpointErr
			}
			if err != nil {
				return err
			}
		}
		_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !networkSelectionApplyOperationMatches(*current, op, plan) {
				return stale()
			}
			current.RPCState.NetworkSelection.TargetRevoked = true
			return nil
		})
		if err != nil {
			return err
		}
		plan.TargetRevoked = true
	}
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	continuity := ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
	if plan.DownStarted {
		if err := stopProfileNetwork(ctx, driver); err != nil {
			return err
		}
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !networkSelectionApplyOperationMatches(*current, op, plan) {
			return stale()
		}
		current.RPCState.NetworkSelection = nil
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		if plan.AbortFailure.Code == ipc.ErrorCode_ERROR_CODE_CANCELLED {
			op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		}
		op.Continuity = continuity
		op.Outcome = &ipc.Operation_Failure{Failure: plan.AbortFailure}
		return nil
	})
	return err
}

func networkSelectionCleanupTargetIsolated(cfg Config, plan *clientRPCNetworkSelection, target Config) bool {
	if target.NetworkID != plan.NetworkID || target.NetworkID == plan.Source.NetworkID || target.ActiveAccountID != plan.Source.ActiveAccountID || target.RPCState != nil {
		return false
	}
	if target.NodeID == "" {
		return true
	}
	if target.NodeID == plan.Source.NodeID || target.NodeID == cfg.NodeID {
		return false
	}
	if cfg.RPCState != nil {
		for _, profile := range cfg.RPCState.Profiles {
			if profile.Configuration.NodeID == target.NodeID {
				return false
			}
		}
	}
	return true
}
