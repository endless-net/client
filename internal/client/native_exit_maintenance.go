package client

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var errNativeExitMaintenance = errors.New("native exit enforcement requires containment")

// maintain never installs a selection or releases protection. The caller must
// hold the shared runtime effect lock and read cfg after acquiring that lock.
// engine.mu additionally keeps path updates from replacing observed effects
// between failed evidence and filter withdrawal/native containment.
func (n *nativeExitExecutor) maintain(ctx context.Context, cfg Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	stopped := !e.configured && e.device == nil && e.router == nil && e.tun == nil
	if e.exitGuard == nil && e.exitSelection == nil {
		return nil
	}
	guard := e.exitGuard
	var err error
	if stopped && guard != nil {
		// A disconnected runtime still owns fail-closed protection. Observation
		// and repair cannot activate its saved selection or release its marker.
		err = guard.ObserveContained(ctx)
	} else {
		var profile string
		profile, err = nativeExitMaintenanceProfile(cfg, e.exitSelection, guard)
		if err == nil {
			_, err = n.observeSelectionLocked(ctx, cfg, e.exitSelection, profile, guard)
		}
	}
	if err == nil {
		return nil
	}
	// The engine-owned pointer, never a guard reconstructed from cfg, determines
	// which protection may be closed. Keep ownership and intent for explicit
	// cleanup/retry; neither observation nor containment grants control authority.
	if e.exitFilter == nil {
		e.exitFilter = &exitPacketFilter{}
	}
	e.exitFilter.withdraw()
	if guard == nil {
		return errors.Join(errNativeExitMaintenance, ctx.Err())
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	containErr := guard.Contain(recovery)
	// Native output is private; preserve failure and cancellation without making
	// successful containment appear to repair the original failed observation.
	if containErr != nil {
		return errors.Join(errNativeExitMaintenance, ctx.Err(), errors.New("native exit containment was not confirmed"))
	}
	return errors.Join(errNativeExitMaintenance, ctx.Err())
}

func nativeExitMaintenanceProfile(cfg Config, actual *ClientExitSelection, guard *linuxExitGuard) (string, error) {
	if actual == nil || guard == nil || cfg.RPCState == nil || cfg.RPCState.ExitProtection == nil {
		return "", errNativeExitMaintenance
	}
	if cfg.ConnectionIntent != nil {
		intent := strings.ToLower(strings.TrimSpace(cfg.ConnectionIntent.DesiredState))
		if intent != "" && intent != ConnectionIntentDesiredConnected {
			return "", errNativeExitMaintenance
		}
	}
	state := cfg.RPCState
	if state.ProfileSwitch != nil || state.DisconnectOperationID != "" || state.Logout != nil || state.Trust != nil || state.NetworkSelection != nil || state.Enrollment != nil {
		return "", errNativeExitMaintenance
	}
	scope := cfg.RPCState.ExitProtection
	profile, exists := state.Profiles[state.ActiveProfileID]
	if !exists || profile.ID == "" || profile.ID != state.ActiveProfileID || scope.OperationID == "" || cfg.LocalOwnerID == "" || cfg.NodeID == "" || cfg.NetworkID == "" {
		return "", errNativeExitMaintenance
	}
	if scope.ProfileID != cfg.RPCState.ActiveProfileID || scope.NodeID != cfg.NodeID || scope.NetworkID != cfg.NetworkID || !strings.EqualFold(scope.OwnerID, cfg.LocalOwnerID) || scope.InterfaceName != guard.interfaceName || scope.RouteTable != cfg.WireGuardRouteTable || scope.RouteTable != actual.RouteTable || actual.NodeID != cfg.NodeID || actual.NetworkID != cfg.NetworkID {
		return "", errNativeExitMaintenance
	}
	table, err := NormalizeWireGuardRouteTable(scope.RouteTable)
	if err != nil || table != scope.RouteTable || table == "off" {
		return "", errNativeExitMaintenance
	}
	mark := uint64(defaultWireGuardEngineRouteTable)
	if table != "" && table != "auto" {
		mark, err = strconv.ParseUint(table, 10, 32)
	}
	if err != nil || !validExitPolicyTable(uint32(mark)) || uint32(mark) != guard.mark {
		return "", errNativeExitMaintenance
	}
	if plan := cfg.RPCState.ExitChange; plan != nil {
		// Apply can succeed before the final durable selection commit. Only the
		// same durably dispatched request authorizes that uncommitted effect.
		if _, err := nativeExitOperation(cfg, plan.OperationID, actual, false); err != nil {
			return "", errNativeExitMaintenance
		}
	} else if !reflect.DeepEqual(cfg.ExitSelection, actual) {
		return "", errNativeExitMaintenance
	}
	return scope.ProfileID, nil
}
