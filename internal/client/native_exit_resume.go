package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

var errNativeExitResume = errors.New("saved exit selection cannot resume in the current context")

// The caller owns the shared effect lock and supplies a fresh store snapshot.
// It must recheck that snapshot after return: admission can commit Disconnect or
// a context transition while native effects run. No operation is manufactured,
// and neither this function nor its failure path releases the existing guard.
func (n *nativeExitExecutor) resumeSaved(ctx context.Context, cfg Config) (status *ipc.ExitNodeStatus, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if n == nil || n.engine == nil || !nativeExitResumeContext(cfg, n.engine.opts.Interface) {
		return nil, errNativeExitResume
	}
	selection := cfg.ExitSelection
	if err = ValidateConfigCurrentDevice(cfg); err != nil {
		return nil, errNativeExitResume
	}
	if _, err = exitRoutePeers(cfg, *cfg.CachedMap, selection, time.Now()); err != nil {
		return nil, errNativeExitResume
	}
	guard, err := n.guard(cfg.RPCState.ExitProtection)
	if err != nil {
		return nil, err
	}
	e := n.engine
	e.mu.Lock()
	live := e.configured || e.device != nil || e.tun != nil
	if e.runtimeSuspended || (live && (e.exitGuard != guard || !reflect.DeepEqual(e.exitSelection, selection) || !sameExitControlIdentity(cfg, e.exitConfig))) {
		e.mu.Unlock()
		return nil, errNativeExitResume
	}
	e.mu.Unlock()
	defer func() {
		if err != nil {
			status = nil
			err = errors.Join(err, n.containFailure(ctx, guard))
		}
	}()
	if live {
		// A failed observation never silently repairs or reopens a running
		// selection. Reapplication after a failure needs an explicit transition.
		return n.observeSelection(ctx, cfg, selection, cfg.RPCState.ActiveProfileID, guard)
	}
	result, err := e.configureExit(ctx, cfg, *cfg.CachedMap, selection, guard)
	if err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, errNativeExitResume
	}
	return n.observeSelection(ctx, cfg, selection, cfg.RPCState.ActiveProfileID, guard)
}

func nativeExitResumeContext(cfg Config, iface string) bool {
	state := cfg.RPCState
	if state == nil || cfg.ExitSelection == nil || state.ExitProtection == nil || cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected || cfg.ConnectionIntent.StartupRecovery != nil || cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || cfg.NodeID == "" || cfg.NetworkID == "" || cfg.NodeCredential == "" || cfg.LocalOwnerID == "" {
		return false
	}
	if state.ExitChange != nil || state.Trust != nil || state.NetworkSelection != nil || state.ProfileSwitch != nil || state.DisconnectOperationID != "" || state.Enrollment != nil || state.Logout != nil || state.SessionRenewal != nil || state.NetworkPreferenceChange != nil || cfg.EnrollmentRecovery != nil || cfg.PendingDirectRegistration != nil || cfg.EnrollmentRequest != nil {
		return false
	}
	profile, exists := state.Profiles[state.ActiveProfileID]
	if !exists || profile.ID != state.ActiveProfileID || profile.ID == "" {
		return false
	}
	urls := cfg.ControlURLs()
	if len(urls) == 0 {
		return false
	}
	origin, err := rpcProfileOrigin(profile.ControlOrigin)
	if err != nil {
		return false
	}
	primary, err := rpcProfileOrigin(urls[0])
	if err != nil || primary != origin {
		return false
	}
	scope, selection := state.ExitProtection, cfg.ExitSelection
	return scope.OperationID != "" && scope.ProfileID == profile.ID && strings.EqualFold(scope.OwnerID, cfg.LocalOwnerID) && scope.NodeID == cfg.NodeID && scope.NetworkID == cfg.NetworkID && scope.InterfaceName == iface && scope.RouteTable == cfg.WireGuardRouteTable && selection.NodeID == cfg.NodeID && selection.NetworkID == cfg.NetworkID && selection.RouteTable == cfg.WireGuardRouteTable && exitGuardFamilyValid(selection.Family) && selection.LAN == api.ExitLANBlock && cfg.MapRevision == cfg.CachedMap.Network.Revision && cfg.MapGlobalRevision == cfg.CachedMap.Revision.Global
}
