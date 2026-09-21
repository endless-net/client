package client

import (
	"context"
	"crypto/hmac"
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileTrustAnnouncement only verifies/checkpoints the confirmed public
// authority. Down, trust adoption and credential recovery are later stages.
func (m *ClientRPCMutations) ReconcileTrustAnnouncement(ctx context.Context, provider ClientRPCServerIdentityProvider) error {
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
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.Trust == nil {
		return nil
	}
	plan := *cfg.RPCState.Trust
	if plan.Announced != nil {
		return nil
	}
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if op.Kind != ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY || op.ProfileId != current.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		return err
	}
	trusted, trustErr := SigningTrustBundle(cfg)
	var failure *ipc.Failure
	if trustErr != nil {
		failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "trusted_identity_changed"}
	}
	announced := trusted
	var providerErr error
	if failure == nil {
		announced, providerErr = provider(ctx, Config{ControlPlaneURLs: []string{plan.ControlOrigin}, MapSigningTrust: &trusted})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failure == nil && (providerErr != nil || announced.Validate() != nil) {
		failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "server_identity_unavailable", Retryable: true}
	}
	if failure == nil {
		id, err := rpcIdentityAnnouncementID(cfg.RPCState.ActiveProfileID, plan.ControlOrigin, trusted, announced)
		if err != nil || id != plan.AnnouncementID || announced.ActiveKeyID != plan.KeyID {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "announcement_confirmation_mismatch"}
		}
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current.RPCState.Trust == nil || current.RPCState.Trust.OperationID != plan.OperationID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if !hmac.Equal(plan.Authority, logoutAuthority(*current)) || !reflect.DeepEqual(cfg.MapSigningTrust, current.MapSigningTrust) || current.RPCState.Profiles[op.ProfileId].ControlOrigin != plan.ControlOrigin {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "local_authority_changed"}
		}
		if failure != nil {
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
			return nil
		}
		bundle := cloneSigningTrustBundle(announced)
		current.RPCState.Trust.Announced = &bundle
		return nil
	})
	return err
}
