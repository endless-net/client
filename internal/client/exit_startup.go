package client

import (
	"context"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// RestoreExitProtection runs before startup control I/O or tunnel application.
// It restores only closed containment; exit readiness and routes are separate.
func (e *WireGuardEngine) RestoreExitProtection(ctx context.Context, cfg Config) error {
	return e.restoreStartupExit(ctx, cfg, newPlatformExitGuard)
}

func (e *WireGuardEngine) restoreStartupExit(ctx context.Context, cfg Config, create func(string, string) (*linuxExitGuard, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.ExitSelection == nil && (cfg.RPCState == nil || cfg.RPCState.ExitChange == nil) {
		return nil
	}
	if queuedExitHasNoEffects(cfg) {
		return nil
	}
	e.mu.Lock()
	guard, name := e.exitGuard, e.opts.Interface
	e.mu.Unlock()
	if guard == nil {
		var err error
		table := cfg.WireGuardRouteTable
		if cfg.ExitSelection != nil {
			table = cfg.ExitSelection.RouteTable
		}
		if cfg.RPCState != nil && cfg.RPCState.ExitChange != nil {
			// A dispatched operation owns its admitted table even if local
			// configuration changed while the process was stopped.
			table = cfg.RPCState.ExitChange.RouteTable
		}
		guard, err = create(name, table)
		if err != nil {
			return err
		}
	}
	return e.restoreExitUnderlay(ctx, cfg, guard)
}

func queuedExitHasNoEffects(cfg Config) bool {
	if cfg.ExitSelection != nil || cfg.RPCState == nil || cfg.RPCState.ExitChange == nil {
		return false
	}
	plan := cfg.RPCState.ExitChange
	if plan.OperationID == "" || plan.Previous != nil || plan.Containing {
		return false
	}
	found := false
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return false
		}
		if op.Id != plan.OperationID {
			continue
		}
		if found || record.CompletedAt != nil || op.State != ipc.OperationState_OPERATION_STATE_PENDING || op.ProfileId != plan.ProfileID {
			return false
		}
		if (plan.Requested != nil && op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE) || (plan.Requested == nil && op.Kind != ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE) {
			return false
		}
		found = true
	}
	return found
}
