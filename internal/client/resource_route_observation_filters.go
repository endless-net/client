package client

import (
	"net/netip"
	"reflect"
	"slices"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Caller holds engine.mu. This is conservative whole-host eligibility: an
// application, sharing or port-only ACL permission never becomes unrestricted
// HOST transport availability. Unrelated protected destinations remain separate.
func resourceHostFilterEligibility(e *WireGuardEngine, cfg Config, now time.Time) (map[string]bool, error) {
	m := cfg.CachedMap
	if m == nil || m.MapSignature == nil || e.peerACLFilter == nil || e.applicationFilter == nil || e.sharingFilter == nil || e.inboundFilter == nil {
		return nil, errResourceHostObservation
	}
	acceptance, err := resolveNetworkAcceptance(cfg, *m, now)
	if err != nil {
		return nil, errResourceHostObservation
	}
	e.inboundFilter.mu.Lock()
	inboundCurrent := e.inboundFilter.blocked == !acceptance.inbound && e.inboundFilter.binding == m.Network.ID+"\x00"+m.Node.ID+"\x00"+m.Node.PublicKey
	e.inboundFilter.mu.Unlock()
	// An intentional block on unsolicited inbound still permits tracked replies;
	// this observation describes outgoing host transport, not inbound hosting.
	if !inboundCurrent {
		return nil, errResourceHostObservation
	}
	peers := m.Peers
	if len(m.Network.Applications) > 0 {
		peers = applicationRoutePeers(*m, now)
	}
	wantACL := compilePeerACL(peers)
	e.peerACLFilter.mu.RLock()
	defer e.peerACLFilter.mu.RUnlock()
	acl := e.peerACLFilter
	if acl.closed || acl.applying || acl.pending != nil || !reflect.DeepEqual(acl.current, wantACL) {
		return nil, errResourceHostObservation
	}
	wantApp := newApplicationPacketFilter()
	wantApp.update(*m)
	app := e.applicationFilter
	app.mu.RLock()
	defer app.mu.RUnlock()
	if !app.signed || !app.expires.Equal(m.MapSignature.ExpiresAt) || !now.Before(app.expires) || !reflect.DeepEqual(app.local, wantApp.local) || !reflect.DeepEqual(app.generic, wantApp.generic) || app.saturated {
		return nil, errResourceHostObservation
	}
	sharing := e.sharingFilter
	sharing.mu.Lock()
	defer sharing.mu.Unlock()
	if sharing.saturated {
		return nil, errResourceHostObservation
	}
	wantSharing := newSharingPacketFilter()
	wantSharing.suspend(*m)
	protected := func(address netip.Addr) bool {
		if sharing.protected[address] || wantSharing.protected[address] {
			return true
		}
		for prefix := range app.protected {
			if prefix.Contains(address) {
				return true
			}
		}
		for prefix := range wantApp.protected {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	for _, source := range app.local {
		if protected(source) {
			return map[string]bool{}, nil
		}
	}
	allowed := map[string]bool{}
	for _, peer := range m.Peers {
		found, eligible := false, true
		for _, text := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(text)
			if err != nil {
				return nil, errResourceHostObservation
			}
			if !prefix.IsSingleIP() {
				continue
			}
			found = true
			target := prefix.Addr()
			if protected(target) || !resourceHostACLUnrestricted(acl.current, target) {
				eligible = false
				break
			}
			// Every observed route source belongs to the committed local set.
			if len(app.local) == 0 || !slices.ContainsFunc(app.local, func(local netip.Addr) bool { return local.Is4() == target.Is4() }) {
				eligible = false
				break
			}
		}
		if found && eligible {
			allowed[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)] = true
		}
	}
	return allowed, nil
}

func resourceHostACLUnrestricted(rules []peerACLRule, target netip.Addr) bool {
	for _, rule := range rules {
		if !rule.destination.Contains(target) {
			continue
		}
		if !rule.deny {
			return true
		}
		for _, grant := range rule.grants {
			if grant.destination.Contains(target) && len(grant.ports) == 0 {
				return true
			}
		}
		return false
	}
	return true
}
