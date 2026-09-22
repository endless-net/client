package client

import (
	"context"
	"errors"
	"reflect"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// The adapter holds the runtime effect lock and the protected bpffs directory
// lock, confirms nft BLOCK, and checkpoints all identities before its first pin
// syscall. This record is recovery metadata, never evidence permitting LAN.
// Replacing/removing a record requires a separate confirmed cleanup operation.
func (m *ClientRPCMutations) checkpointExitLANOwnership(ctx context.Context, id string, expected *clientRPCExitProtection, ownership *exitLANOwnership) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected == nil || validateExitLANOwnership(ownership) != nil {
		return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	expected = cloneExitProtection(expected)
	ownership = cloneExitLANOwnership(ownership)
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		plan := cfg.RPCState.ExitChange
		if !exitChangeBound(cfg, plan, op) || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || plan.Containing || plan.Releasing || plan.Protection == nil || plan.Requested == nil || plan.Requested.LAN != api.ExitLANAllow || plan.Requested.Family != ownership.Family || !reflect.DeepEqual(plan.Protection, expected) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if plan.Protection.LAN != nil {
			if !reflect.DeepEqual(plan.Protection.LAN, ownership) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			return errRPCNoChange
		}
		plan.Protection = cloneExitProtection(plan.Protection)
		plan.Protection.LAN = cloneExitLANOwnership(ownership)
		cfg.RPCState.ExitProtection = cloneExitProtection(plan.Protection)
		return ctx.Err()
	})
	if errors.Is(err, errRPCNoChange) {
		return nil
	}
	return err
}
