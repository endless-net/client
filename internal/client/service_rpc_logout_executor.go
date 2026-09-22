package client

import (
	"context"
	"crypto/hmac"
	"errors"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type ClientRPCLogoutProvider func(context.Context, Config, ClientRPCLogoutProgress, func(ClientRPCLogoutProgress) error) (string, error)

func (m *ClientRPCMutations) ReconcileLogout(ctx context.Context, driver ClientRPCProfileDriver, provider ClientRPCLogoutProvider) error {
	if provider == nil || driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
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
	if cfg.RPCState == nil || cfg.RPCState.Logout == nil {
		return nil
	}
	plan := *cfg.RPCState.Logout
	_, err := m.ReconcileOperation(plan.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if !logoutPlanBound(*cfg, &plan) || op.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT || op.ProfileId != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		return err
	}
	cfg = m.store.Read()
	if !logoutPlanBound(cfg, &plan) {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	logoutCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancelLogout = cancel
	current := m.store.Read()
	if current.RPCState.DisconnectOperationID != "" {
		cancel()
	}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.cancelLogout = nil; m.mu.Unlock(); cancel() }()
	if !logoutPlanBound(current, &plan) {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if logoutCtx.Err() != nil {
		return ctx.Err()
	}
	var checkpointErr error
	checkpoint := m.LogoutProgressCallback(plan.OperationID, cfg)
	requestID, remoteErr := provider(logoutCtx, cfg, plan.Progress, func(progress ClientRPCLogoutProgress) error {
		if checkpointErr == nil {
			checkpointErr = checkpoint(progress)
		}
		return checkpointErr
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if logoutCtx.Err() != nil {
		return nil
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	continuity := ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
	var stopErr error
	if remoteErr == nil {
		current := m.store.Read()
		if !logoutPlanBound(current, &plan) || !current.RPCState.Logout.Progress.NodeRevoked || !current.RPCState.Logout.Progress.SessionRevoked {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if _, err := m.ReconcileOperation(plan.OperationID, func(current *Config, _ *ipc.Operation) error {
			if !logoutPlanBound(*current, &plan) || !hmac.Equal(logoutAuthority(*current), logoutAuthority(cfg)) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			current.RPCState.Logout.DownStarted = true
			return nil
		}); err != nil {
			return err
		}
		continuity, stopErr = driver.Stop(logoutCtx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if logoutCtx.Err() != nil {
			return nil
		}
		if stopErr == nil && !profileStopConfirmed(continuity) {
			stopErr = rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		if plan.DownStarted && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
			continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		}
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := logoutCtx.Err(); err != nil {
			return err
		}
		if !logoutPlanBound(*current, &plan) || op.ProfileId != current.RPCState.ActiveProfileID || !hmac.Equal(logoutAuthority(*current), logoutAuthority(cfg)) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if remoteErr != nil || stopErr != nil {
			if remoteErr != nil {
				profile := current.RPCState.Profiles[op.ProfileId]
				profile.LogoutConfirmation = &clientRPCLogoutConfirmation{NetworkID: current.NetworkID, Authority: logoutAuthority(*current), Progress: current.RPCState.Logout.Progress, RequestID: requestID}
				current.RPCState.Profiles[profile.ID] = profile
			}
			failure := &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED, ReasonKey: "remote_cleanup_unconfirmed", ControlRequestId: requestID}
			if stopErr != nil {
				failure.Code, failure.ReasonKey = ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, "logout_down_failed"
			}
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
			return nil
		}
		if err := ApplyLocalLogoutCleanup(current, m.now()); err != nil {
			return err
		}
		profile := current.RPCState.Profiles[op.ProfileId]
		if err := ApplyLocalLogoutCleanup(&profile.Configuration, m.now()); err != nil {
			return err
		}
		profile.LogoutConfirmation = nil
		current.RPCState.Profiles[profile.ID] = profile
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		if continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE {
			continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		}
		op.Continuity = continuity
		op.Outcome = &ipc.Operation_Cleanup{Cleanup: &ipc.CleanupResult{Outcome: ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_CONFIRMED, LocalRegistrationRemoved: true, ControlRequestId: requestID}}
		return nil
	})
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return nil
	}
	return err
}
