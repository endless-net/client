package client

import (
	"slices"
	"time"
)

// TryResourceEnforcement confirms that the current TUN filter has committed
// exactly this authenticated resource configuration. It does not establish
// routes, peer health, DNS success or end-to-end resource reachability.
// Busy Configure/Down returns unknown without blocking the caller.
func (e *WireGuardEngine) TryResourceEnforcement(cfg Config, now time.Time) bool {
	rules, err := compileResourceDenials(cfg, now)
	if err != nil {
		return false
	}
	if !e.mu.TryLock() {
		return false
	}
	defer e.mu.Unlock()
	source := cfg.CachedMap
	if !e.configured || e.device == nil || e.resourceFilter == nil || e.pathMap.MapSignature == nil || e.pathMap.MapSignature.PayloadHash != source.MapSignature.PayloadHash || e.pathMap.Node.ID != cfg.NodeID || e.pathMap.Network.ID != cfg.NetworkID {
		return false
	}
	filter := e.resourceFilter
	filter.mu.RLock()
	defer filter.mu.RUnlock()
	return filter.active && !filter.closed && !filter.applying && now.Before(filter.expires) && filter.expires.Equal(source.MapSignature.ExpiresAt) && slices.Equal(filter.current, rules)
}
