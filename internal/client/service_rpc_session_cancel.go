package client

import (
	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func (m *ClientRPCMutations) sessionRenewalCancellationRequested() bool {
	cfg := m.store.Read()
	return cfg.RPCState != nil && cfg.RPCState.SessionRenewal != nil && cfg.RPCState.SessionRenewal.CancelRequested
}

// Caller holds sessionWorker: no renewal provider can still be executing.
// Forget retains its cancellation marker across crash until this step commits.
func (m *ClientRPCMutations) cancelSessionRenewalForForget() error {
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.SessionRenewal == nil {
		return nil
	}
	if !cfg.RPCState.SessionRenewal.CancelRequested {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	_, err := m.ReconcileOperation(cfg.RPCState.SessionRenewal.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if cfg.RPCState.SessionRenewal == nil || !cfg.RPCState.SessionRenewal.CancelRequested || cfg.RPCState.SessionRenewal.OperationID != op.Id {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		setSessionRenewalForgotten(cfg, op)
		return nil
	})
	return err
}

func setSessionRenewalForgotten(cfg *Config, op *ipc.Operation) {
	cfg.RPCState.SessionRenewal = nil
	op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
	op.UserAction = nil
	op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED, ReasonKey: "session_renewal_forgotten"}}
}
