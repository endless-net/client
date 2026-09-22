package client

import (
	"context"
	"errors"
	"reflect"
)

// Caller holds the shared effect lock and has stopped the owned engine under
// nft containment. Only confirmed native absence authorizes retiring metadata.
func (n *nativeExitExecutor) cleanupLAN(ctx context.Context, scope *clientRPCExitProtection, guard *linuxExitGuard) error {
	if scope == nil || scope.LAN == nil {
		return nil
	}
	if n.store == nil || n.cleanupLANObjects == nil {
		return errExitLANBPF
	}
	expected := cloneExitProtection(scope)
	if err := n.cleanupLANObjects(ctx, guard, cloneExitLANOwnership(expected.LAN)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return n.store.Update(func(cfg *Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cfg.RPCState == nil || !reflect.DeepEqual(cfg.RPCState.ExitProtection, expected) {
			return errors.New("LAN cleanup protection changed")
		}
		plan := cfg.RPCState.ExitChange
		if plan != nil && !reflect.DeepEqual(plan.Protection, expected) {
			return errors.New("LAN cleanup operation changed")
		}
		cfg.RPCState.ExitProtection.LAN = nil
		if plan != nil {
			plan.Protection.LAN = nil
		}
		return nil
	})
}
