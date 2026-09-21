package client

import (
	"context"
	"errors"
	"time"
)

// Called under the host's effect lock, with the guard recovered from durable
// protection ownership. A stopped engine does not imply that kernel rules died
// with the previous process. This step never releases firewall protection.
func (e *WireGuardEngine) recoverStoppedExit(ctx context.Context, guard *linuxExitGuard) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recoverStoppedExitLocked(ctx, guard)
}

func (e *WireGuardEngine) recoverStoppedExitLocked(ctx context.Context, guard *linuxExitGuard) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard == nil || e.exitGuard != guard || guard.mark == 0 || guard.mark == 253 || guard.mark == 254 || guard.mark == 255 {
		return errors.New("exit recovery requires its dedicated owned scope")
	}
	if e.device != nil || e.router != nil || e.tun != nil || e.configured {
		return errors.New("exit recovery requires a stopped runtime")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	e.exitFilter.withdraw()
	if e.exitRestoreConfig.NodeID == "" && e.exitConfig.NodeID != "" {
		e.exitRestoreConfig = clonePersistentConfig(e.exitConfig)
	}
	e.exitConfig = Config{}
	e.clearUnderlayDNSLocked()
	if err := guard.Contain(ctx); err != nil {
		return err
	}
	if err := recoverExitOwnedDefaultRoutes(ctx, guard); err != nil {
		return err
	}
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return guard.run(ctx, "", name, args...)
	}
	for _, family := range []string{"-4", "-6"} {
		for _, suppress := range []bool{false, true} {
			if err := removeLinuxPolicyRule(ctx, runner, family, guard.mark, suppress); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
