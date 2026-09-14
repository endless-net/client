package client

import (
	"context"
	"reflect"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Trusted platform evidence for the original operation context. The adapter
// must serialize with all route/identity changes, remove the affected exit
// routes and retain blocking in BOTH families before reporting this proof.
type clientRPCExitContainment struct {
	OperationID, ProfileID, NodeID, NetworkID   string
	IPv4Blocked, IPv6Blocked, ExitRoutesRemoved bool
}

func markExitContainment(cfg *Config, plan *clientRPCExitChange, code ipc.ErrorCode, now time.Time) {
	plan.Containing = true
	plan.FailureCode, plan.FailureReason = code, "exit_context_changed"
	if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredDisconnected && !reflect.DeepEqual(cfg.ConnectionIntent, plan.PreviousIntent) {
		plan.FailureCode, plan.FailureReason = ipc.ErrorCode_ERROR_CODE_CANCELLED, "exit_superseded_by_disconnect"
	}
	// Never rewrite a different profile/identity's connection intent.
	if cfg.RPCState.ActiveProfileID == plan.ProfileID && cfg.NodeID == plan.NodeID && cfg.NetworkID == plan.NetworkID && strings.EqualFold(cfg.LocalOwnerID, plan.OwnerID) && (cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected) {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "exit_containment", UpdatedAt: now.UTC().Format(time.RFC3339Nano)}
	}
}

func (m *ClientRPCMutations) containExitChange(ctx context.Context, id string, executor clientRPCExitExecutor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.ExitChange == nil || cfg.RPCState.ExitChange.OperationID != id {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	plan := *cfg.RPCState.ExitChange
	if !plan.Containing || plan.FailureCode == 0 || executor.Contain == nil {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	proof, err := executor.Contain(ctx, plan)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if proof.OperationID != id || proof.ProfileID != plan.ProfileID || proof.NodeID != plan.NodeID || proof.NetworkID != plan.NetworkID || !proof.IPv4Blocked || !proof.IPv6Blocked || !proof.ExitRoutesRemoved {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	_, err = m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if !reflect.DeepEqual(cfg.RPCState.ExitChange, &plan) || rpcOperationTerminal(op.State) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		cfg.RPCState.ExitChange = nil
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		if plan.FailureCode == ipc.ErrorCode_ERROR_CODE_CANCELLED {
			op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		}
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: plan.FailureCode, ReasonKey: plan.FailureReason}}
		return nil
	})
	return err
}
