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
	InterfaceName string
	// The same lock used by connection, profile and map application. Callbacks
	// run under it and must not acquire it again. Hold through durable completion
	// or containment so another OS operation cannot invalidate an uncommitted
	// observation between Apply returning and completeExitChange.
	Lock    *sync.Mutex
	Modes   []clientRPCExitMode
	Apply   func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error)
	Contain func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error)
	// Release runs only after the durable clear checkpoint. It must repeat OS
	// cleanup/readback safely after restart and retain containment on ambiguity.
	Release func(context.Context, string, Config) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error)
	// Observe runs under Lock and rechecks native enforcement independently of
	// a previous successful operation. It does not mutate durable intent.
	Observe func(context.Context, Config) (*ipc.ExitNodeStatus, error)
	// Maintain rechecks active enforcement without applying or resuming intent.
	// The caller holds Lock and reads Config only after acquiring it.
	Maintain func(context.Context, Config) error
	// ResumeSaved restores only a committed connected selection, without
	// manufacturing a new mutation. The caller must recheck durable context.
	ResumeSaved func(context.Context, Config) (*ipc.ExitNodeStatus, error)
}

func exitChangeBound(cfg *Config, plan *clientRPCExitChange, op *ipc.Operation) bool {
	if plan == nil || cfg.RPCState == nil || op == nil {
		return false
	}
	profile, exists := cfg.RPCState.Profiles[plan.ProfileID]
	if plan.ProfileID != plan.ActiveProfileID && !reflect.DeepEqual(plan.PreviousProfileSelection, profile.Configuration.ExitSelection) {
		return false
	}
	// A still-valid grant in a replacement map does not prove that the OS
	// applied that map. Clear remains independent of withdrawn map authority.
	return exists && plan.OperationID == op.Id && plan.ProfileID == op.ProfileId && plan.ActiveProfileID == cfg.RPCState.ActiveProfileID && exitProtectionBound(cfg, plan) &&
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
	if strings.TrimSpace(executor.InterfaceName) != executor.InterfaceName || !safeWireGuardInterfaceName(executor.InterfaceName) || executor.InterfaceName == "lo" {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if !lockExitRuntime(ctx, &m.exitWorker) {
		return ctx.Err()
	}
	defer m.exitWorker.Unlock()
	if !lockExitRuntime(ctx, executor.Lock) {
		return ctx.Err()
	}
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
	if executor.Apply == nil && !cfg.RPCState.ExitChange.Releasing {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	var input Config
	var selection *ClientExitSelection
	var releasing bool
	ready := false
	contain := false
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		plan := cfg.RPCState.ExitChange
		if plan == nil || plan.OperationID != id || rpcOperationTerminal(op.State) {
			return errRPCNoChange
		}
		code := ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
		if !exitChangeBound(cfg, plan, op) || (plan.Releasing && plan.Requested != nil) || (plan.Protection != nil && plan.Protection.InterfaceName != executor.InterfaceName) {
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
		if plan.Protection == nil {
			plan.Protection = &clientRPCExitProtection{OperationID: id, ProfileID: plan.ProfileID, OwnerID: plan.OwnerID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, InterfaceName: executor.InterfaceName, RouteTable: plan.RouteTable}
			cfg.RPCState.ExitProtection = cloneExitProtection(plan.Protection)
		}
		plan.NextAttemptAt = m.now().Add(5 * time.Second)
		selection = cloneExitSelection(plan.Requested)
		releasing = plan.Releasing
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
	// ReconcileOperation serializes its changed operation only after its callback
	// returns. The dispatch snapshot captured above therefore still contains the
	// prior serialized operation. Read the committed RUNNING record under the
	// mutation lock, without substituting a changed journal or authority scope.
	if !releasing {
		ready, contain = false, false
		_, err = m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			plan := cfg.RPCState.ExitChange
			if plan == nil || plan.OperationID != id || rpcOperationTerminal(op.State) {
				return errRPCNoChange
			}
			if plan.Containing {
				contain = true
				return errRPCNoChange
			}
			if op.State != ipc.OperationState_OPERATION_STATE_RUNNING || !reflect.DeepEqual(plan, input.RPCState.ExitChange) || !exitChangeBound(cfg, plan, op) {
				markExitContainment(cfg, plan, ipc.ErrorCode_ERROR_CODE_STALE_STATE, m.now())
				contain = true
				return nil
			}
			input = clonePersistentConfig(*cfg)
			ready = true
			return errRPCNoChange
		})
		if errors.Is(err, errRPCNoChange) {
			err = nil
		}
		if err == nil && contain {
			return m.containExitChange(ctx, id, executor)
		}
		if err != nil || !ready {
			return err
		}
	}
	var observed *ipc.ExitNodeStatus
	var continuity ipc.ConnectionContinuity
	if !releasing {
		observed, continuity, err = executor.Apply(ctx, id, input, selection)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		// Do not disclose native error text or discard possible OS effects.
		return nil
	}
	if selection == nil {
		if !releasing {
			err = m.checkpointExitRelease(ctx, id, observed)
		}
		if err == nil {
			if executor.Release == nil {
				return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			input, err = m.exitReleaseInput(ctx, id)
			if err == nil {
				observed, continuity, err = executor.Release(ctx, id, input)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil {
					return nil
				}
			}
		}
	}
	if err == nil {
		_, err = m.completeExitChange(id, observed, continuity)
	}
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
