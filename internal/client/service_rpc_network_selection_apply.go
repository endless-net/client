package client

import (
	"context"
	"reflect"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileNetworkSelectionApply owns the activated target until either apply
// succeeds or cleanup is confirmed. A failed cleanup keeps the durable barrier.
func (m *ClientRPCMutations) ReconcileNetworkSelectionApply(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Stop == nil || driver.Start == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if !lockExitRuntime(ctx, &m.networkSelectionWorker) {
		return ctx.Err()
	}
	defer m.networkSelectionWorker.Unlock()
	if !lockExitRuntime(ctx, &m.profileWorker) {
		return ctx.Err()
	}
	defer m.profileWorker.Unlock()
	if !lockExitRuntime(ctx, driver.Lock) {
		return ctx.Err()
	}
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	if !plan.Activated || !plan.DownStarted || plan.Target == nil {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	failure := plan.ApplyFailure
	resuming := plan.ApplyStarted
	if failure == nil {
		if !networkSelectionActiveTargetMatches(cfg, plan) || !networkSelectionTargetReady(cfg, m.now()) {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "network_selection_target_changed"}
		} else {
			_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !networkSelectionApplyOperationMatches(*current, op, plan) || !networkSelectionActiveTargetMatches(*current, plan) {
					return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
				}
				current.RPCState.NetworkSelection.ApplyStarted = true
				return nil
			})
			if err != nil {
				return err
			}
			plan.ApplyStarted = true
			connected := cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected
			if resuming || !connected {
				if err := stopProfileNetwork(ctx, driver); err != nil {
					return err
				}
			}
			if connected {
				if err := m.applyNetworkSelectionTarget(ctx, driver, cfg, plan); err != nil {
					failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "network_selection_apply_failed"}
				}
			}
			if err := ctx.Err(); err != nil {
				return err // Recovery must stop an uncertain previous apply first.
			}
			if failure == nil {
				_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					if !networkSelectionApplyOperationMatches(*current, op, plan) || !networkSelectionActiveTargetMatches(*current, plan) || !networkSelectionTargetReady(*current, m.now()) {
						return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
					}
					current.RPCState.NetworkSelection = nil
					op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
					op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: plan.NetworkID}}
					return nil
				})
				if err == nil {
					return nil
				}
				failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "network_selection_target_changed"}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Commit cleanup intent before Down: after a crash a failed apply is never
	// retried as a fresh successful selection. Do not publish terminal failure
	// until the driver confirms removal of any partially installed target.
	_, checkpointErr := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !networkSelectionApplyOperationMatches(*current, op, plan) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if plan.ApplyFailure == nil && current.ConnectionIntent != nil && current.ConnectionIntent.DesiredState == ConnectionIntentDesiredDisconnected && !reflect.DeepEqual(current.ConnectionIntent, plan.Target.ConnectionIntent) {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED, ReasonKey: "network_selection_superseded_by_disconnect"}
		}
		current.RPCState.NetworkSelection.ApplyFailure = failure
		return nil
	})
	if err := stopProfileNetwork(ctx, driver); err != nil {
		return err
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	plan.ApplyFailure = failure
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !networkSelectionApplyOperationMatches(*current, op, plan) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		// Preserve newer owner/context/intent decisions. Only disconnect the
		// exact failed target that this operation attempted to apply.
		if networkSelectionActiveTargetMatches(*current, plan) {
			current.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "network_selection_apply_failed", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
			profile := current.RPCState.Profiles[plan.Profile.ID]
			profile.Configuration = profileConfiguration(*current)
			current.RPCState.Profiles[profile.ID] = profile
		}
		current.RPCState.NetworkSelection = nil
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		if failure.Code == ipc.ErrorCode_ERROR_CODE_CANCELLED {
			op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		}
		op.Outcome = &ipc.Operation_Failure{Failure: failure}
		return nil
	})
	return err
}

func networkSelectionActiveTargetMatches(cfg Config, plan *clientRPCNetworkSelection) bool {
	if cfg.RPCState == nil || plan.Target == nil || cfg.RPCState.ActiveProfileID != plan.Profile.ID || !reflect.DeepEqual(networkSelectionContext(cfg), *plan.Target) {
		return false
	}
	profile := plan.Profile
	profile.Configuration = profileConfiguration(*plan.Target)
	return reflect.DeepEqual(cfg.RPCState.Profiles[plan.Profile.ID], profile)
}

func networkSelectionApplyOperationMatches(cfg Config, op *ipc.Operation, plan *clientRPCNetworkSelection) bool {
	return cfg.RPCState != nil && reflect.DeepEqual(cfg.RPCState.NetworkSelection, plan) &&
		op.Id == plan.OperationID && op.Kind == ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK && op.ProfileId == plan.Profile.ID && op.State == ipc.OperationState_OPERATION_STATE_RUNNING
}

func stopProfileNetwork(ctx context.Context, driver ClientRPCProfileDriver) error {
	continuity, err := driver.Stop(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if !profileStopConfirmed(continuity) {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	return nil
}

func (m *ClientRPCMutations) applyNetworkSelectionTarget(ctx context.Context, driver ClientRPCProfileDriver, cfg Config, plan *clientRPCNetworkSelection) error {
	m.mu.Lock()
	current := m.store.Read()
	if !networkSelectionActiveTargetMatches(current, plan) || !reflect.DeepEqual(current.RPCState.NetworkSelection, plan) || !networkSelectionTargetReady(current, m.now()) ||
		current.ConnectionIntent == nil || current.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
		m.mu.Unlock()
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	applyCtx, cancel := context.WithCancel(ctx)
	m.cancelApply = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.cancelApply = nil
		m.mu.Unlock()
	}()
	return driver.Start(applyCtx, cfg)
}
