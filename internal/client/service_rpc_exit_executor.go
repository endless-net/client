package client

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// The platform adapter must serialize with connection/map application and
// install protection before routes. Apply is idempotent for the operation ID;
// errors (including cancellation) must retain protection, never fall back to
// direct routing. No adapter is advertised until these OS effects exist.
type clientRPCExitExecutor struct {
	// The same lock used by connection, profile and map application. Callbacks
	// run under it and must not acquire it again. Hold through durable completion
	// or containment so another OS operation cannot invalidate an uncommitted
	// observation between Apply returning and completeExitChange.
	Lock    *sync.Mutex
	Modes   []clientRPCExitMode
	Apply   func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error)
	Contain func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error)
}

func exitChangeBound(cfg *Config, plan *clientRPCExitChange, op *ipc.Operation) bool {
	if plan == nil || cfg.RPCState == nil || op == nil {
		return false
	}
	profile, exists := cfg.RPCState.Profiles[plan.ProfileID]
	// A still-valid grant in a replacement map does not prove that the OS
	// applied that map. Clear remains independent of withdrawn map authority.
	return exists && plan.OperationID == op.Id && plan.ProfileID == op.ProfileId && plan.ProfileID == cfg.RPCState.ActiveProfileID &&
		strings.EqualFold(plan.OwnerID, cfg.LocalOwnerID) && plan.ControlOrigin == profile.ControlOrigin &&
		plan.NodeID == cfg.NodeID && plan.NetworkID == cfg.NetworkID && plan.RouteTable == cfg.WireGuardRouteTable && reflect.DeepEqual(plan.Previous, cfg.ExitSelection) &&
		reflect.DeepEqual(plan.PreviousIntent, cfg.ConnectionIntent) &&
		(plan.Requested == nil || (plan.MapHash != "" && cfg.CachedMap != nil && cfg.CachedMap.MapSignature != nil && plan.MapHash == cfg.CachedMap.MapSignature.PayloadHash)) &&
		((plan.Requested == nil && op.Kind == ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE) ||
			(plan.Requested != nil && op.Kind == ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE))
}

// One serialized attempt. RUNNING and the retry deadline commit before any OS
// call. Ambiguous/partial results retain the same durable operation for recovery;
// they cannot commit selection, claim success, or unblock conflicting changes.
func (m *ClientRPCMutations) reconcileExitChange(ctx context.Context, executor clientRPCExitExecutor) error {
	if executor.Lock == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.exitWorker.Lock()
	defer m.exitWorker.Unlock()
	executor.Lock.Lock()
	defer executor.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.ExitChange == nil {
		return nil
	}
	id := cfg.RPCState.ExitChange.OperationID
	if cfg.RPCState.ExitChange.Containing {
		return m.containExitChange(ctx, id, executor)
	}
	if executor.Apply == nil {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	var input Config
	var selection *ClientExitSelection
	ready := false
	contain := false
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		plan := cfg.RPCState.ExitChange
		if plan == nil || plan.OperationID != id || rpcOperationTerminal(op.State) {
			return errRPCNoChange
		}
		code := ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
		if !exitChangeBound(cfg, plan, op) {
			code = ipc.ErrorCode_ERROR_CODE_STALE_STATE
		} else if plan.Requested != nil {
			if !slices.Contains(executor.Modes, clientRPCExitMode{Family: plan.Requested.Family, LAN: plan.Requested.LAN}) {
				code = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			} else if cfg.CachedMap == nil || cfg.CachedMap.Network.Revision != cfg.MapRevision || cfg.CachedMap.Revision.Global != cfg.MapGlobalRevision {
				code = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			} else if _, err := exitRoutePeers(*cfg, *cfg.CachedMap, plan.Requested, m.now()); err != nil {
				code = ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED
			}
		}
		if code != ipc.ErrorCode_ERROR_CODE_UNSPECIFIED {
			// A queued operation has no effects. A previously dispatched one
			// needs OS containment/recovery before its guard may be released.
			if op.State != ipc.OperationState_OPERATION_STATE_PENDING {
				markExitContainment(cfg, plan, code, m.now())
				contain = true
				return nil
			}
			cfg.RPCState.ExitChange = nil
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: code, ReasonKey: "exit_preflight_rejected"}}
			return nil
		}
		if m.now().Before(plan.NextAttemptAt) {
			return errRPCNoChange
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		plan.NextAttemptAt = m.now().Add(5 * time.Second)
		selection = cloneExitSelection(plan.Requested)
		input = clonePersistentConfig(*cfg)
		ready = true
		return nil
	})
	if errors.Is(err, errRPCNoChange) {
		return nil
	}
	if err == nil && contain {
		return m.containExitChange(ctx, id, executor)
	}
	if err != nil || !ready {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	observed, continuity, err := executor.Apply(ctx, id, input, selection)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		// Do not disclose native error text or discard possible OS effects.
		return nil
	}
	_, err = m.completeExitChange(id, observed, continuity)
	if err != nil {
		if failure := rpc.FailureFromError(err); failure != nil && failure.Code == ipc.ErrorCode_ERROR_CODE_STALE_STATE {
			_, checkpointErr := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
				plan := cfg.RPCState.ExitChange
				if plan == nil || plan.OperationID != id || rpcOperationTerminal(op.State) {
					return errRPCNoChange
				}
				bound := exitChangeBound(cfg, plan, op)
				if bound && plan.Requested != nil {
					bound = cfg.CachedMap != nil && cfg.MapRevision == cfg.CachedMap.Network.Revision && cfg.MapGlobalRevision == cfg.CachedMap.Revision.Global
					if bound {
						_, validationErr := exitRoutePeers(*cfg, *cfg.CachedMap, plan.Requested, m.now())
						bound = validationErr == nil
					}
				}
				if bound {
					return errRPCNoChange
				}
				markExitContainment(cfg, plan, ipc.ErrorCode_ERROR_CODE_STALE_STATE, m.now())
				return nil
			})
			if errors.Is(checkpointErr, errRPCNoChange) {
				// The context is still authorized, but the adapter has not
				// supplied complete enforcement evidence. Retain the original
				// operation/deadline and let the worker retry, just as for an
				// ambiguous Apply error. STALE_STATE would terminate the worker.
				return nil
			}
			if checkpointErr != nil {
				return checkpointErr
			}
			return m.containExitChange(ctx, id, executor)
		}
	}
	return err
}
