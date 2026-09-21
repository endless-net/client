package client

import "strings"

// This records an address for future read-only absence checks. It grants no
// routing/control authority and is never itself evidence of native cleanup.
// Artifact identity can differ from current identity when clearing an inactive
// profile's old protection. Neither identity contains credentials.
type clientRPCExitCleared struct {
	OperationID         string                   `json:"operation_id"`
	Protection          *clientRPCExitProtection `json:"protection"`
	ActiveProfileID     string                   `json:"active_profile_id"`
	ActiveControlOrigin string                   `json:"active_control_origin"`
	OwnerID             string                   `json:"owner_id"`
	NodeID              string                   `json:"node_id"`
	NetworkID           string                   `json:"network_id"`
	RouteTable          string                   `json:"route_table"`
	ControlURLs         []string                 `json:"control_urls,omitempty"`
}

func recordClearedExitScope(cfg *Config, plan *clientRPCExitChange) {
	cfg.RPCState.ExitCleared = &clientRPCExitCleared{OperationID: plan.OperationID,
		Protection: cloneExitProtection(plan.Protection), ActiveProfileID: cfg.RPCState.ActiveProfileID,
		ActiveControlOrigin: cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID].ControlOrigin,
		OwnerID:             cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable,
		ControlURLs: append([]string(nil), cfg.ControlURLs()...)}
}

func (r *clientRPCExitCleared) bound(cfg Config, iface string) bool {
	if r == nil || r.OperationID == "" || r.Protection == nil || cfg.RPCState == nil || cfg.ExitSelection != nil || cfg.RPCState.ExitChange != nil || cfg.RPCState.ExitProtection != nil || r.ActiveProfileID == "" || cfg.RPCState.ActiveProfileID != r.ActiveProfileID || !strings.EqualFold(cfg.LocalOwnerID, r.OwnerID) || cfg.NodeID != r.NodeID || cfg.NetworkID != r.NetworkID || cfg.WireGuardRouteTable != r.RouteTable {
		return false
	}
	scope := r.Protection
	if scope.OperationID == "" || scope.ProfileID == "" || scope.NodeID == "" || scope.NetworkID == "" || scope.InterfaceName != iface || !strings.EqualFold(scope.OwnerID, r.OwnerID) || r.OwnerID == "" {
		return false
	}
	profile, exists := cfg.RPCState.Profiles[r.ActiveProfileID]
	if !exists || profile.ID != r.ActiveProfileID || profile.ControlOrigin != r.ActiveControlOrigin {
		return false
	}
	origins := cfg.ControlURLs()
	if len(origins) != len(r.ControlURLs) {
		return false
	}
	for i, origin := range origins {
		if origin != r.ControlURLs[i] {
			return false
		}
	}
	return true
}
