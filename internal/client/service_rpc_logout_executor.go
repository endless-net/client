package client

import (
	"context"
	"crypto/hmac"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type ClientRPCLogoutProvider func(context.Context, Config, ClientRPCLogoutProgress, func(ClientRPCLogoutProgress) error) (string, error)

func (m *ClientRPCMutations) ReconcileLogout(ctx context.Context, driver ClientRPCProfileDriver, provider ClientRPCLogoutProvider) error {
	if provider == nil || driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
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
		if op.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT || op.ProfileId != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		return err
	}
	cfg = m.store.Read()
	var checkpointErr error
	checkpoint := m.LogoutProgressCallback(plan.OperationID, cfg)
	requestID, remoteErr := provider(ctx, cfg, plan.Progress, func(progress ClientRPCLogoutProgress) error {
		if checkpointErr == nil {
			checkpointErr = checkpoint(progress)
		}
		return checkpointErr
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	continuity := ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
	var stopErr error
	if remoteErr == nil {
		current := m.store.Read()
		if current.RPCState.Logout == nil || !current.RPCState.Logout.Progress.NodeRevoked || !current.RPCState.Logout.Progress.SessionRevoked {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if _, err := m.ReconcileOperation(plan.OperationID, func(current *Config, _ *ipc.Operation) error {
			if !hmac.Equal(logoutAuthority(*current), logoutAuthority(cfg)) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			current.RPCState.Logout.DownStarted = true
			return nil
		}); err != nil {
			return err
		}
		continuity, stopErr = driver.Stop(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if plan.DownStarted && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
			continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		}
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if op.ProfileId != current.RPCState.ActiveProfileID || !hmac.Equal(logoutAuthority(*current), logoutAuthority(cfg)) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if remoteErr != nil || stopErr != nil {
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
	return err
}
