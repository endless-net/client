package connectivity

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"slices"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// LocalDomainDiscovery identifies a signed request for this router to resolve
// a name. It grants no packet path before Coordinator approves addresses.
func LocalDomainDiscovery(app coordinator.Application, local coordinator.ServiceHost) bool {
	if app.TargetType != "domain" || !identifier(app.ID) || len(app.Routes) != 0 || len(app.Sources) != 0 ||
		len(app.Connectors) != 1 || app.Connectors[0] != local || !identifier(local.NodeID) || !wireGuardKey(local.PublicKey) ||
		len(app.PolicyHash) != sha256.Size*2 {
		return false
	}
	if _, ok := dnsDomain(app.Target); !ok {
		return false
	}
	return validDiscoveryHash(app.PolicyHash)
}

func validDiscoveryHash(value string) bool {
	hash, err := hex.DecodeString(value)
	return err == nil && len(hash) == sha256.Size && hex.EncodeToString(hash) == value
}

// RouteDestinations validates each advertised host for a domain. A router
// can carry only the subset that Coordinator signed for its own key.
func RouteDestinations(app coordinator.Application, route coordinator.ApplicationRoute, overlay netip.Prefix) ([]netip.Prefix, error) {
	if app.TargetType != "domain" {
		p, err := AssignmentDestination(app, overlay)
		if err != nil || len(route.CIDRs) != 1 || route.CIDRs[0] != p.String() {
			return nil, ErrMap
		}
		return []netip.Prefix{p}, nil
	}
	if _, ok := dnsDomain(app.Target); !ok || !validDiscoveryHash(app.PolicyHash) || !overlay.Addr().Is4() || len(route.CIDRs) == 0 || len(route.CIDRs) > 64 {
		return nil, ErrMap
	}
	result := make([]netip.Prefix, 0, len(route.CIDRs))
	for _, raw := range route.CIDRs {
		p, err := netip.ParsePrefix(raw)
		if err != nil || !p.Addr().Is4() || !p.Addr().IsGlobalUnicast() || p.Bits() != 32 || p.String() != raw || overlay.Contains(p.Addr()) || slices.Contains(result, p) {
			return nil, ErrMap
		}
		result = append(result, p)
	}
	return result, nil
}

// UnavailableResourcePrefixes returns the destinations a recipient must keep
// off its physical interface even when no connector has a usable route.
func UnavailableResourcePrefixes(snapshot coordinator.NetworkMapSnapshot, limit int) ([]netip.Prefix, error) {
	return unavailableResourcePrefixes(snapshot, limit, false)
}

// UnavailableExitPrefixes also retains the internet guard when a recipient's
// explicit exit assignment temporarily loses all routers.
func UnavailableExitPrefixes(snapshot coordinator.NetworkMapSnapshot, limit int) ([]netip.Prefix, error) {
	return unavailableResourcePrefixes(snapshot, limit, true)
}

func unavailableResourcePrefixes(snapshot coordinator.NetworkMapSnapshot, limit int, exit bool) ([]netip.Prefix, error) {
	network := snapshot.Network
	if len(network.Applications) == 0 {
		return nil, nil
	}
	overlay, err := netip.ParsePrefix(network.CIDR)
	if err != nil || len(network.Applications) > limit {
		return nil, ErrMap
	}
	var prefixes []netip.Prefix
	seen := make(map[netip.Prefix]bool)
	for _, app := range network.Applications {
		if !identifier(app.ID) || len(app.Routes)+len(app.Sources) != 0 {
			return nil, ErrMap
		}
		identity := coordinator.ServiceHost{NodeID: snapshot.Node.ID, PublicKey: snapshot.Node.PublicKey}
		if LocalDomainDiscovery(app, identity) {
			continue
		}
		if app.TargetType == "domain" && len(app.Connectors) == 0 && app.DNSEnabled && validDiscoveryHash(app.PolicyHash) {
			if _, ok := dnsDomain(app.Target); ok {
				continue
			}
		}
		p, err := RouterDestination(app, overlay, identity)
		if exit && len(app.Connectors) == 0 {
			p, err = AssignmentDestination(app, overlay)
		}
		if err != nil {
			return nil, err
		}
		if len(app.Connectors) > 0 {
			// The assigned router retains its ordinary LAN access when its
			// advertisement ends. No Routes or Sources authorize transit.
			if !identifier(identity.NodeID) || !wireGuardKey(identity.PublicKey) || len(app.Connectors) != 1 || app.Connectors[0] != identity {
				return nil, ErrMap
			}
			continue
		}
		if !seen[p] {
			prefixes = append(prefixes, p)
			seen[p] = true
		}
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int { return a.Compare(b) })
	return prefixes, nil
}

// ResourceDestination validates an IPv4 resource outside the overlay.
func ResourceDestination(app coordinator.Application, overlay netip.Prefix) (netip.Prefix, error) {
	var p netip.Prefix
	var err error
	switch app.TargetType {
	case "ip":
		address, err := netip.ParseAddr(app.Target)
		if err != nil || address.String() != app.Target {
			return netip.Prefix{}, ErrMap
		}
		p = netip.PrefixFrom(address, address.BitLen())
	case "cidr":
		p, err = netip.ParsePrefix(app.Target)
		if err != nil || p.Masked().String() != app.Target {
			return netip.Prefix{}, ErrMap
		}
	default:
		return netip.Prefix{}, ErrMap
	}
	if !overlay.Addr().Is4() || overlay != overlay.Masked() || !p.Addr().Is4() || p.Bits() == 0 || p.Overlaps(overlay) {
		return netip.Prefix{}, ErrMap
	}

	return p, nil
}

// AssignmentDestination includes the explicit IPv4 exit in signed resource assignments.
func AssignmentDestination(app coordinator.Application, overlay netip.Prefix) (netip.Prefix, error) {
	if app.TargetType == "cidr" && app.Target == "0.0.0.0/0" && overlay.Addr().Is4() && overlay.Bits() > 0 && overlay == overlay.Masked() {
		return netip.PrefixFrom(netip.IPv4Unspecified(), 0), nil
	}
	return ResourceDestination(app, overlay)
}

// RouterDestination admits an explicit default assignment only for the
// named forwarding node. A recipient needs separate native default routing.
func RouterDestination(app coordinator.Application, overlay netip.Prefix, local coordinator.ServiceHost) (netip.Prefix, error) {
	if identifier(local.NodeID) && wireGuardKey(local.PublicKey) && slices.Contains(app.Connectors, local) {
		return AssignmentDestination(app, overlay)
	}
	return ResourceDestination(app, overlay)
}

// ResourcePath binds the exact signed resource prefix to one eligible peer.
// A caller supplying live path health must identify the node and its key.
type ResourcePath struct {
	ApplicationID string
	Prefix        netip.Prefix
	Route         coordinator.ApplicationRoute
}

// AssignedPrefixes includes unreachable paths so a broader grant cannot
// authorize traffic to a more specific resource when its router is down.
func AssignedPrefixes(app coordinator.Application, overlay netip.Prefix) ([]netip.Prefix, error) {
	if app.TargetType != "domain" {
		p, err := AssignmentDestination(app, overlay)
		if err != nil {
			return nil, err
		}
		return []netip.Prefix{p}, nil
	}
	var prefixes []netip.Prefix
	seen := make(map[netip.Prefix]bool)
	for _, route := range app.Routes {
		ps, err := RouteDestinations(app, route, overlay)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if !seen[p] {
				seen[p] = true
				prefixes = append(prefixes, p)
			}
		}
	}
	return prefixes, nil
}

// SelectResourcePaths selects the lowest metric, then the lowest node ID on
// ties. A resource with no reachable, unexpired path has no selected entry.
func SelectResourcePaths(snapshot coordinator.NetworkMapSnapshot, at time.Time, reachable func(coordinator.ServiceHost) bool) ([]ResourcePath, error) {
	if !slices.ContainsFunc(snapshot.Network.Applications, func(app coordinator.Application) bool { return len(app.Routes) > 0 }) {
		return nil, nil
	}
	overlay, err := netip.ParsePrefix(snapshot.Network.CIDR)
	if err != nil || !overlay.Addr().Is4() || overlay != overlay.Masked() {
		return nil, ErrMap
	}
	peers := make(map[string]coordinator.Peer, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		if !identifier(peer.ID) || peers[peer.ID].ID != "" {
			return nil, ErrMap
		}
		peers[peer.ID] = peer
	}
	local := coordinator.ServiceHost{NodeID: snapshot.Node.ID, PublicKey: snapshot.Node.PublicKey}
	selected := make([]ResourcePath, 0)
	ids := map[string]bool{}
	owners := map[netip.Prefix]coordinator.ServiceHost{}
	for _, app := range snapshot.Network.Applications {
		if !identifier(app.ID) || ids[app.ID] {
			return nil, ErrMap
		}
		ids[app.ID] = true
		if len(app.Routes) == 0 {
			continue
		}
		if len(app.AllowedPorts) == 0 || at.IsZero() {
			return nil, ErrMap
		}
		for _, p := range app.AllowedPorts {
			if !(coordinator.ACLPort{Protocol: p.Protocol, Port: int(p.Port)}).Valid() {
				return nil, ErrMap
			}
		}
		hosts := map[string]coordinator.ServiceHost{}
		for _, h := range app.Connectors {
			if !identifier(h.NodeID) || !wireGuardKey(h.PublicKey) || hosts[h.NodeID].NodeID != "" {
				return nil, ErrMap
			}
			if h != local && peers[h.NodeID].PublicKey != h.PublicKey {
				return nil, ErrMap
			}
			hosts[h.NodeID] = h
		}
		best := map[netip.Prefix]*coordinator.ApplicationRoute{}
		seen := map[string]bool{}
		for i := range app.Routes {
			r := &app.Routes[i]
			h := r.Connector
			prefixes, err := RouteDestinations(app, *r, overlay)
			if err != nil || !identifier(h.NodeID) || hosts[h.NodeID] != h || seen[h.NodeID] || r.Metric < 1 || r.Metric > 9999 || r.ExpiresAt.IsZero() {
				return nil, ErrMap
			}
			seen[h.NodeID] = true
			if h == local {
				continue
			}
			p := peers[h.NodeID]
			if p.EndpointExpiresAt == nil || p.EndpointExpiresAt.Before(r.ExpiresAt) {
				return nil, ErrMap
			}
			if !at.Before(r.ExpiresAt) || reachable != nil && !reachable(h) {
				continue
			}
			for _, p := range prefixes {
				prev := best[p]
				if prev == nil || r.Metric < prev.Metric || r.Metric == prev.Metric && h.NodeID < prev.Connector.NodeID {
					best[p] = r
				}
			}
		}
		for id, h := range hosts {
			if h != local && !seen[id] {
				return nil, ErrMap
			}
		}
		// A router reaches this destination on its LAN, including when a
		// second router is healthy. Never point its prefix back into the mesh.
		if h, ok := hosts[local.NodeID]; ok && h == local {
			continue
		}
		if len(app.Sources) != 0 {
			return nil, ErrMap
		}
		start := len(selected)
		for prefix, route := range best {
			if owner, ok := owners[prefix]; ok && owner != route.Connector {
				return nil, ErrMap
			}
			owners[prefix] = route.Connector
			selected = append(selected, ResourcePath{app.ID, prefix, *route})
		}
		slices.SortFunc(selected[start:], func(a, b ResourcePath) int { return a.Prefix.Compare(b.Prefix) })
	}
	return selected, nil
}

// Bound native expansion before exposing an active resource map. Every key
// and advertisement needed by an installed path must cover the signed lease.
func validateIPv4ResourceRoutes(m coordinator.NetworkMapSnapshot, limit int, forwarding, exit bool) error {
	if len(m.Network.Applications) > limit || m.MapSignature == nil {
		return ErrMap
	}
	if _, err := SelectResourcePaths(m, m.MapSignature.IssuedAt, nil); err != nil {
		return err
	}
	inactive := m
	inactive.Network.Applications = nil
	peers := make(map[string]coordinator.Peer, len(m.Peers))
	for _, peer := range m.Peers {
		peers[peer.ID] = peer
	}
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	remaining := limit
	for _, app := range m.Network.Applications {
		if len(app.Routes) == 0 {
			inactive.Network.Applications = append(inactive.Network.Applications, app)
			continue
		}
		isRouter := slices.Contains(app.Connectors, local)
		// Native recipients must isolate their control sockets before installing a default.
		if (!forwarding && isRouter) || (app.Target == "0.0.0.0/0" && !isRouter && !exit) {
			return ErrMap
		}
		own := false
		for _, route := range app.Routes {
			if route.ExpiresAt.Before(m.MapSignature.ExpiresAt) {
				return ErrMap
			}
			if route.Connector == local {
				if m.Node.EndpointExpiresAt == nil || m.Node.EndpointExpiresAt.Before(route.ExpiresAt) {
					return ErrMap
				}
				own = true
			}
		}
		if len(app.Sources) > 0 && !own {
			return ErrMap
		}
		seen := make(map[string]bool, len(app.Sources))
		for _, source := range app.Sources {
			peer, ok := peers[source.NodeID]
			if !ok || source.NodeID == local.NodeID || source.PublicKey != peer.PublicKey || seen[source.NodeID] || peer.EndpointExpiresAt == nil || peer.EndpointExpiresAt.Before(m.MapSignature.ExpiresAt) {
				return ErrMap
			}
			seen[source.NodeID] = true
		}
		count := max(1, len(app.Sources))
		if app.TargetType == "domain" {
			overlay, _ := netip.ParsePrefix(m.Network.CIDR)
			prefixes, err := AssignedPrefixes(app, overlay)
			if err != nil || len(prefixes) == 0 || len(prefixes) > remaining/count {
				return ErrMap
			}
			count *= len(prefixes)
		}
		if len(app.AllowedPorts) > remaining/count {
			return ErrMap
		}
		remaining -= len(app.AllowedPorts) * count
	}
	_, err := unavailableResourcePrefixes(inactive, limit, exit)
	return err
}
