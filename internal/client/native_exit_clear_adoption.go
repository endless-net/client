package client

import (
	"strings"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// A non-secret projection of the last successful engine apply. pathMap alone
// cannot distinguish two local owners/profiles that use the same node keys.
type nativeExitRuntimeIdentity struct {
	OwnerID, ProfileID, ControlOrigin string
	NodeID, NetworkID, PublicKey      string
	InterfaceName, RouteTable         string
}

func nativeExitAppliedIdentity(cfg Config, network api.RegisterNodeResponse, iface string) nativeExitRuntimeIdentity {
	identity := nativeExitRuntimeIdentity{OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, PublicKey: network.Node.PublicKey, InterfaceName: iface, RouteTable: cfg.WireGuardRouteTable}
	if cfg.RPCState != nil {
		identity.ProfileID = cfg.RPCState.ActiveProfileID
		identity.ControlOrigin = cfg.RPCState.Profiles[identity.ProfileID].ControlOrigin
	}
	return identity
}

// Caller holds engine.mu. Adoption is restricted to a fully committed ordinary
// runtime. Partial cleanup and foreign/missing ownership cannot authorize a new
// guard or a teardown merely because Config now names a different owner.
func (e *WireGuardEngine) ordinaryExitClearOwnedLocked(scope *clientRPCExitProtection, origin string) bool {
	if scope == nil || e.exitGuard != nil || e.exitSelection != nil || !e.configured || e.device == nil || e.tun == nil || e.router == nil || e.interface_ != scope.InterfaceName || e.routerCfg.Interface != scope.InterfaceName || e.routerCfg.RouteTable != scope.RouteTable || e.routerCfg.FirewallMark != 0 {
		return false
	}
	identity := e.runtimeIdentity
	if identity.OwnerID == "" || identity.ProfileID == "" || identity.ControlOrigin == "" || origin != identity.ControlOrigin || !strings.EqualFold(identity.OwnerID, scope.OwnerID) || identity.ProfileID != scope.ProfileID || identity.NodeID != scope.NodeID || identity.NetworkID != scope.NetworkID || identity.InterfaceName != scope.InterfaceName || identity.RouteTable != scope.RouteTable || identity.PublicKey == "" || e.pathMap.Node.ID != identity.NodeID || e.pathMap.Network.ID != identity.NetworkID || e.pathMap.Node.PublicKey != identity.PublicKey {
		return false
	}
	for _, route := range e.routerCfg.Routes {
		if !route.IsValid() || route.Bits() == 0 {
			return false
		}
	}
	raw, err := e.device.IpcGet()
	return err == nil && nativeExitClearedUAPI(e.uapi, raw, identity.PublicKey) == nil
}
