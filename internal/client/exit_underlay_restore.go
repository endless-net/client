package client

import (
	"context"
	"errors"
)

// restoreExitUnderlay is the native adapter's pre-network recovery step for an
// existing durable selection. It restores containment and control authority,
// not exit routes or an applied selection. Call under the shared effect lock.
func (e *WireGuardEngine) restoreExitUnderlay(ctx context.Context, cfg Config, guard *linuxExitGuard) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard == nil || guard.mark == 0 || (e.exitGuard != nil && e.exitGuard != guard) || e.device != nil || e.router != nil || e.tun != nil || e.configured {
		return errors.New("exit underlay recovery requires an owned guard and stopped runtime")
	}
	if cfg.ExitSelection == nil || cfg.NodeID == "" || cfg.NodeCredential == "" || cfg.NetworkID == "" || cfg.ExitSelection.NodeID != cfg.NodeID || cfg.ExitSelection.NetworkID != cfg.NetworkID {
		return errors.New("exit underlay recovery requires a bound durable selection")
	}
	if err := ValidateConfigCurrentDevice(cfg); err != nil {
		return err
	}
	// Validate the entire origin set before taking ownership; construction opens
	// no sockets. The guard supplies the mark even with no router configuration.
	control, err := newControlUnderlayHTTPClient(cfg.ControlURLs(), guard.mark, nil)
	if err != nil {
		return err
	}
	control.CloseIdleConnections()
	if e.exitGuard != nil && e.exitConfig.NodeID != "" && !sameExitControlIdentity(cfg, e.exitConfig) {
		return errors.New("exit underlay recovery cannot replace an owned identity")
	}
	if e.exitRestoreConfig.NodeID != "" && !sameExitControlIdentity(cfg, e.exitRestoreConfig) {
		return errors.New("exit underlay recovery cannot replace its pending identity")
	}
	e.exitGuard = guard
	e.exitRestoreConfig = clonePersistentConfig(cfg)
	e.exitSelection = nil
	e.exitConfig = Config{}
	if e.exitFilter == nil {
		e.exitFilter = &exitPacketFilter{}
	}
	e.exitFilter.withdraw()
	if err := guard.Contain(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.exitConfig = clonePersistentConfig(cfg)
	e.exitRestoreConfig = Config{}
	return nil
}
