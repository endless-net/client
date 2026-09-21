package client

import (
	"errors"
	"net/netip"
	"slices"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitLANPrefixLimit = 8192
const exitLANWorkLimit = 1 << 20

var errExitLANPolicy = errors.New("LAN destination scope is unavailable")

// This is input to a future native route/firewall transaction, not permission
// to bypass the TUN. Topology alone supplies neither exit health nor a kernel
// lease or packet-time interface/next-hop binding.
type exitLANReservation struct {
	prefixes []netip.Prefix
	expires  time.Time
}

// Reserve destinations independently of local enablement, route acceptance,
// ACLs and application expiry. Those restrictions must never reveal a direct
// LAN fallback. Default exit routes are not resource reservations.
func compileExitLANReservation(cfg Config, source api.RegisterNodeResponse, selection *ClientExitSelection, retained []netip.Prefix, now time.Time) (*exitLANReservation, error) {
	if selection == nil || selection.LAN != api.ExitLANAllow || len(retained) > exitLANPrefixLimit {
		return nil, errExitLANPolicy
	}
	policy, err := compileExitPacketPolicy(cfg, source, selection, now)
	if err != nil {
		return nil, errExitLANPolicy
	}
	if _, _, err := resourceDenialsForMap(cfg, source, now); err != nil {
		return nil, errExitLANPolicy
	}
	result := &exitLANReservation{expires: policy.mapExpires}
	if policy.grantExpires.Before(result.expires) {
		result.expires = policy.grantExpires
	}
	if !now.Before(result.expires) {
		return nil, errExitLANPolicy
	}
	seen := map[netip.Prefix]bool{}
	add := func(prefix netip.Prefix) error {
		if !prefix.IsValid() || prefix.Addr().Is4In6() {
			return errExitLANPolicy
		}
		prefix = prefix.Masked()
		if !seen[prefix] {
			if len(seen) >= exitLANPrefixLimit {
				return errExitLANPolicy
			}
			seen[prefix] = true
			result.prefixes = append(result.prefixes, prefix)
		}
		return nil
	}
	addRoute := func(value string) error {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return errExitLANPolicy
		}
		if prefix.Bits() == 0 {
			return nil
		}
		return add(prefix)
	}
	for _, value := range []string{"0.0.0.0/32", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255/32", "::/128", "::1/128", "fe80::/10", "ff00::/8", "::ffff:0:0/96"} {
		// IPv4-mapped IPv6 destinations are not valid native LAN identities.
		prefix := netip.MustParsePrefix(value)
		if prefix.Addr().Is4In6() {
			seen[prefix] = true
			result.prefixes = append(result.prefixes, prefix)
		} else if err := add(prefix); err != nil {
			return nil, err
		}
	}
	for _, value := range []string{source.Network.CIDR, source.Network.IPv6CIDR} {
		if value != "" {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Bits() == 0 || add(prefix) != nil {
				return nil, errExitLANPolicy
			}
		}
	}
	for _, value := range source.Node.AdvertisedIPs {
		if err := addRoute(value); err != nil {
			return nil, err
		}
	}
	for _, peer := range source.Peers {
		for _, value := range peer.AllowedIPs {
			if err := addRoute(value); err != nil {
				return nil, err
			}
		}
	}
	for _, app := range source.Network.Applications {
		target, err := api.ParseApplicationTarget(app.TargetType, app.Target)
		if err != nil {
			return nil, errExitLANPolicy
		}
		if target.Prefix.IsValid() {
			if err := add(target.Prefix); err != nil {
				return nil, err
			}
		}
		for _, route := range app.Routes {
			for _, value := range route.CIDRs {
				prefix, err := netip.ParsePrefix(value)
				if err != nil || add(prefix) != nil {
					return nil, errExitLANPolicy
				}
			}
		}
	}
	for _, resource := range source.Network.ClientPolicy.Resources {
		if resource.Kind == api.ManagedResourceSubnet {
			if err := addRoute(resource.CIDR); err != nil {
				return nil, err
			}
		}
	}
	for _, prefix := range retained {
		if err := add(prefix); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(result.prefixes, exitLANComparePrefix)
	return result, nil
}

func exitLANComparePrefix(a, b netip.Prefix) int {
	if result := a.Addr().Compare(b.Addr()); result != 0 {
		return result
	}
	return a.Bits() - b.Bits()
}

// Caller holds engine.mu. Sticky TUN reservations survive map withdrawal and
// must also be excluded from any later LAN scope until that runtime is torn down.
func (e *WireGuardEngine) retainedExitLANDestinationsLocked() ([]netip.Prefix, error) {
	result := []netip.Prefix{}
	if f := e.applicationFilter; f != nil {
		f.mu.RLock()
		if f.saturated {
			f.mu.RUnlock()
			return nil, errExitLANPolicy
		}
		for prefix := range f.protected {
			result = append(result, prefix)
		}
		f.mu.RUnlock()
	}
	if f := e.sharingFilter; f != nil {
		f.mu.Lock()
		if f.saturated {
			f.mu.Unlock()
			return nil, errExitLANPolicy
		}
		for address := range f.protected {
			result = append(result, netip.PrefixFrom(address, address.BitLen()))
		}
		f.mu.Unlock()
	}
	if len(result) > exitLANPrefixLimit {
		return nil, errExitLANPolicy
	}
	slices.SortFunc(result, exitLANComparePrefix)
	return slices.Compact(result), nil
}

// Compute exact CIDR subtraction, never a covering aggregate. The original
// connected prefix remains the attachment binding; fragments are destinations
// within it. /31 and /32 have no conventional IPv4 network/broadcast endpoints.
func (r *exitLANReservation) destinations(prefix netip.Prefix, now time.Time) ([]netip.Prefix, error) {
	work := 0
	return r.destinationsWithinBudget(prefix, now, &work)
}

func (r *exitLANReservation) destinationsWithinBudget(prefix netip.Prefix, now time.Time, work *int) ([]netip.Prefix, error) {
	if r == nil || !now.Before(r.expires) || !prefix.IsValid() || prefix.Addr().Is4In6() || prefix != prefix.Masked() || prefix.Bits() == 0 || len(r.prefixes) > exitLANPrefixLimit {
		return nil, errExitLANPolicy
	}
	endpoints := []netip.Prefix{}
	if prefix.Addr().Is4() && prefix.Bits() <= 30 {
		endpoints = append(endpoints, netip.PrefixFrom(prefix.Addr(), 32))
		last := prefix.Addr().As4()
		for bit := prefix.Bits(); bit < 32; bit++ {
			last[bit/8] |= 1 << (7 - bit%8)
		}
		endpoints = append(endpoints, netip.PrefixFrom(netip.AddrFrom4(last), 32))
	}
	result := []netip.Prefix{prefix}
	for i := range len(r.prefixes) + len(endpoints) {
		if len(result) == 0 {
			return nil, nil
		}
		var deny netip.Prefix
		if i < len(r.prefixes) {
			deny = r.prefixes[i]
		} else {
			deny = endpoints[i-len(r.prefixes)]
		}
		if !deny.IsValid() {
			return nil, errExitLANPolicy
		}
		next := make([]netip.Prefix, 0, len(result))
		var subtract func(netip.Prefix) error
		subtract = func(current netip.Prefix) error {
			*work = *work + 1
			if *work > exitLANWorkLimit {
				return errExitLANPolicy
			}
			if !current.Overlaps(deny) {
				if len(next) >= exitLANPrefixLimit {
					return errExitLANPolicy
				}
				next = append(next, current)
				return nil
			}
			if deny.Bits() <= current.Bits() {
				return nil
			}
			bits := current.Bits()
			left := netip.PrefixFrom(current.Addr(), bits+1)
			var right netip.Addr
			if current.Addr().Is4() {
				bytes := current.Addr().As4()
				bytes[bits/8] |= 1 << (7 - bits%8)
				right = netip.AddrFrom4(bytes)
			} else {
				bytes := current.Addr().As16()
				bytes[bits/8] |= 1 << (7 - bits%8)
				right = netip.AddrFrom16(bytes)
			}
			if err := subtract(left); err != nil {
				return err
			}
			return subtract(netip.PrefixFrom(right, bits+1))
		}
		for _, current := range result {
			if err := subtract(current); err != nil {
				return nil, err
			}
		}
		result = next
	}
	return result, nil
}
