package dataplane

import (
	"net/netip"
	"slices"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"go4.org/netipx"
)

// Both the plaintext switch and native firewall derive the initiating peer,
// overlay source address and service port from the same local assignment.
func visitRouterGrants(snapshot coordinator.NetworkMapSnapshot, app coordinator.Application, local coordinator.ServiceHost, peers map[string]coordinator.Peer,
	visit func(netip.Prefix, string, netip.Addr, coordinator.ACLPort, bool, time.Time) error) error {
	if snapshot.MapSignature == nil {
		return ErrConfiguration
	}
	until := snapshot.MapSignature.ExpiresAt
	for _, route := range app.Routes {
		if route.Connector != local {
			continue
		}
		if route.ExpiresAt.Before(until) || snapshot.Node.EndpointExpiresAt == nil || snapshot.Node.EndpointExpiresAt.Before(route.ExpiresAt) {
			return ErrConfiguration
		}
		overlay, _ := netip.ParsePrefix(snapshot.Network.CIDR)
		prefixes, err := connectivity.RouteDestinations(app, route, overlay)
		if err != nil {
			return ErrConfiguration
		}
		seen := make(map[string]bool, len(app.Sources))
		for _, source := range app.Sources {
			peer, ok := peers[source.NodeID]
			if !ok || source.NodeID == local.NodeID || source.PublicKey != peer.PublicKey || seen[source.NodeID] || len(peer.AllowedIPs) != 1 || peer.EndpointExpiresAt == nil || peer.EndpointExpiresAt.Before(until) {
				return ErrConfiguration
			}
			seen[source.NodeID] = true
			address, err := netip.ParsePrefix(peer.AllowedIPs[0])
			if err != nil || address.Bits() != 32 || !address.Addr().Is4() {
				return ErrConfiguration
			}
			for _, port := range app.AllowedPorts {
				for _, prefix := range prefixes {
					if err := visit(prefix, source.NodeID, address.Addr(), coordinator.ACLPort{Protocol: port.Protocol, Port: int(port.Port)}, route.Masquerade, until); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if len(app.Sources) > 0 {
		return ErrConfiguration
	}
	return nil
}

type routerPacketRules map[netip.Prefix]map[netip.Addr]resourcePacketRule

// A default transit grant covers internet egress. Access to these local
// address spaces needs a separately assigned, more-specific resource.
var exitLocalNetworks = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
}

// Both native route planners cover the same public IPv4 destinations as
// the packet switch. The overlay and local networks keep their own routes.
func exitPublicPrefixes(overlay netip.Prefix) ([]netip.Prefix, error) {
	if !overlay.IsValid() || !overlay.Addr().Is4() || overlay.Bits() == 0 || overlay != overlay.Masked() {
		return nil, ErrConfiguration
	}
	var set netipx.IPSetBuilder
	set.AddPrefix(netip.PrefixFrom(netip.IPv4Unspecified(), 0))
	set.RemovePrefix(overlay)
	for _, local := range exitLocalNetworks {
		set.RemovePrefix(local)
	}
	addresses, err := set.IPSet()
	if err != nil {
		return nil, err
	}
	return addresses.Prefixes(), nil
}

func (r resourcePacketRule) internetRule(resource netip.Addr) resourcePacketRule {
	if r.peer != "" && r.exitOverlay.IsValid() && !r.exitOverlay.Contains(resource) && !slices.ContainsFunc(exitLocalNetworks[:], func(p netip.Prefix) bool { return p.Contains(resource) }) {
		return r
	}
	return resourcePacketRule{}
}

func (rules routerPacketRules) lookup(resource, source netip.Addr) resourcePacketRule {
	if len(rules) > 0 && resource.Is4() {
		for bits := 32; bits > 0; bits-- {
			if sources, ok := rules[netip.PrefixFrom(resource, bits).Masked()]; ok {
				return sources[source]
			}
		}
		rule := rules[netip.PrefixFrom(netip.IPv4Unspecified(), 0)][source]
		return rule.internetRule(resource)
	}
	return resourcePacketRule{}
}

func routerPacketPolicy(m coordinator.NetworkMapSnapshot) (routerPacketRules, map[string]bool, error) {
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	if !slices.ContainsFunc(m.Network.Applications, func(a coordinator.Application) bool { return slices.Contains(a.Connectors, local) }) {
		return nil, nil, nil
	}
	overlay, err := netip.ParsePrefix(m.Network.CIDR)
	if err != nil {
		return nil, nil, ErrConfiguration
	}
	peers := make(map[string]coordinator.Peer, len(m.Peers))
	for _, p := range m.Peers {
		peers[p.ID] = p
	}
	rules, subjects := make(routerPacketRules), make(map[string]bool)
	for _, app := range m.Network.Applications {
		if connectivity.LocalDomainDiscovery(app, local) {
			continue
		}
		prefixes, err := connectivity.AssignedPrefixes(app, overlay)
		if err != nil {
			return nil, nil, ErrConfiguration
		}
		for _, prefix := range prefixes {
			if rules[prefix] != nil {
				continue
			}
			rules[prefix] = make(map[netip.Addr]resourcePacketRule)
		}
		if !slices.Contains(app.Connectors, local) || len(app.Routes) == 0 {
			continue
		}
		err = visitRouterGrants(m, app, local, peers, func(prefix netip.Prefix, id string, source netip.Addr, port coordinator.ACLPort, _ bool, end time.Time) error {
			r := rules[prefix][source]
			if r.peer == "" {
				r = resourcePacketRule{peer: id, end: end, ports: make(map[coordinator.ACLPort]bool)}
			}
			if r.peer != id {
				return ErrConfiguration
			}
			if prefix.Bits() == 0 {
				r.exitOverlay = overlay
			}
			r.ports[port] = true
			rules[prefix][source] = r
			subjects[id] = true
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return rules, subjects, nil
}

func peerPacketDeadline(a candidateAssignment, rules resourcePacketRules, transit routerPacketRules, raw []byte, outbound bool) (time.Time, error) {
	local, peer, err := a.addresses()
	if err != nil {
		return time.Time{}, err
	}
	src, err := packetAddress(raw, 12)
	if err != nil {
		return time.Time{}, err
	}
	dst, err := packetAddress(raw, 16)
	if err != nil {
		return time.Time{}, err
	}
	if err := applicationForPair(raw, src, dst); err != nil {
		return time.Time{}, err
	}
	near, remote := src, dst
	if !outbound {
		near, remote = dst, src
	}
	if near != local {
		r := transit.lookup(near, remote)
		if remote != peer || r.peer != a.peer.ID || !r.permits(raw, !outbound) || r.end.IsZero() {
			return time.Time{}, ErrNoAuthority
		}
		return r.end, nil
	}
	if remote == peer && pairGranted(a.peer) {
		return time.Time{}, nil
	}
	rule := rules.lookup(remote)
	if rule.peer != a.peer.ID || !rule.permits(raw, outbound) || rule.end.IsZero() {
		return time.Time{}, ErrNoAuthority
	}
	return rule.end, nil
}
