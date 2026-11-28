package connectivity

import (
	"net/netip"
	"slices"
	"strings"
	"time"

	contract "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/net/dns/dnsmessage"
)

type serviceAddress struct {
	record dnsmessage.Resource
	until  time.Time
}

// MapDNSConfig returns a resolver assignment even when the only routed names
// come from the authorized service directory.
func MapDNSConfig(m contract.NetworkMapSnapshot) *contract.DNSConfig {
	n := m.Network
	if n.DNSConfig != nil {
		return n.DNSConfig
	}
	if HasMapDNSDirectory(m) {
		return &contract.DNSConfig{}
	}
	return nil
}

// HasMapDNSDirectory identifies local answers that change with the signed
// map even when upstream resolvers and search domains stay identical.
func HasMapDNSDirectory(m contract.NetworkMapSnapshot) bool {
	return len(m.Network.Services) > 0 || slices.ContainsFunc(m.Network.Applications, func(app contract.Application) bool { return recipientDomain(m, app) })
}

func recipientDomain(m contract.NetworkMapSnapshot, app contract.Application) bool {
	return app.TargetType == "domain" && app.DNSEnabled && !slices.Contains(app.Connectors, contract.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey})
}

type servicePeerAuthority struct {
	ip   [4]byte
	ends map[contract.ACLPort]time.Time
}

func servicePeerAuthorities(m contract.NetworkMapSnapshot) map[contract.ServiceHost]servicePeerAuthority {
	peers := map[contract.ServiceHost]servicePeerAuthority{}
	for _, p := range m.Peers {
		if !p.ACLRestricted || p.NetworkID != m.Network.ID || len(p.AllowedIPs) != 1 {
			continue
		}
		prefix, err := netip.ParsePrefix(p.AllowedIPs[0])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		ends := map[contract.ACLPort]time.Time{}
		for _, g := range p.ACLGrants {
			if len(g.DestinationCIDRs) != 1 || g.DestinationCIDRs[0] != p.AllowedIPs[0] {
				continue
			}
			end := m.MapSignature.ExpiresAt
			if g.ValidUntil != nil && g.ValidUntil.Before(end) {
				end = *g.ValidUntil
			}
			if !end.After(m.MapSignature.IssuedAt) {
				continue
			}
			for _, port := range g.AllowedPorts {
				if end.After(ends[port]) {
					ends[port] = end
				}
			}
		}
		peers[contract.ServiceHost{NodeID: p.ID, PublicKey: p.PublicKey}] = servicePeerAuthority{prefix.Addr().As4(), ends}
	}
	return peers
}

// CompileMapDNS derives local A answers from approved host keys and individual
// outbound ACLs. A query checks the last applicable grant for each host.
func CompileMapDNS(m contract.NetworkMapSnapshot, current func(time.Time) bool) (DNSRoutes, error) {
	r, err := CompileDNSRoutes(MapDNSConfig(m))
	if err != nil || !HasMapDNSDirectory(m) {
		return r, err
	}
	if m.MapSignature == nil {
		return DNSRoutes{}, ErrMap
	}
	r.services = make(map[string][]serviceAddress)
	r.current = current
	peers := servicePeerAuthorities(m)
	ids := map[string]bool{}
	for _, svc := range m.Network.Services {
		name, ok := dnsDomain(svc.DNSName)
		if !identifier(svc.ID) || ids[svc.ID] || !ok || r.services[name] != nil || svc.Name == "" || len(svc.Ports) == 0 || len(svc.Hosts) == 0 {
			return DNSRoutes{}, ErrMap
		}
		ids[svc.ID] = true
		if len(r.local[r.match(name)][name]) != 0 {
			return DNSRoutes{}, ErrDNSAssignment
		}
		owner, err := dnsmessage.NewName(name + ".")
		if err != nil {
			return DNSRoutes{}, ErrMap
		}
		ports := map[contract.ACLPort]bool{}
		for _, p := range svc.Ports {
			port := contract.ACLPort{Protocol: p.Protocol, Port: int(p.Port)}
			if _, duplicate := ports[port]; duplicate || p.Protocol == contract.ProtocolAll || !port.Valid() {
				return DNSRoutes{}, ErrMap
			}
			ports[port] = false
		}
		seen := map[contract.ServiceHost]bool{}
		for _, h := range svc.Hosts {
			host, ok := peers[h]
			if !ok || seen[h] {
				return DNSRoutes{}, ErrMap
			}
			seen[h] = true
			all := host.ends[contract.ACLPort{Protocol: contract.ProtocolAll}]
			latest := all
			for port := range ports {
				end := host.ends[port]
				if !all.IsZero() || !end.IsZero() {
					ports[port] = true
				}
				if end.After(latest) {
					latest = end
				}
			}
			if latest.IsZero() {
				return DNSRoutes{}, ErrMap
			}
			rr := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: owner, Class: dnsmessage.ClassINET, Type: dnsmessage.TypeA, TTL: 0}, Body: &dnsmessage.AResource{A: host.ip}}
			r.services[name] = append(r.services[name], serviceAddress{rr, latest})
		}
		for _, matched := range ports {
			if !matched {
				return DNSRoutes{}, ErrMap
			}
		}
	}
	overlay, _ := netip.ParsePrefix(m.Network.CIDR)
	for _, app := range m.Network.Applications {
		if !recipientDomain(m, app) {
			continue
		}
		name, ok := dnsDomain(app.Target)
		if !ok || r.services[name] != nil || len(r.local[r.match(name)][name]) != 0 {
			return DNSRoutes{}, ErrDNSAssignment
		}
		owner, err := dnsmessage.NewName(name + ".")
		if err != nil {
			return DNSRoutes{}, ErrDNSAssignment
		}
		r.services[name] = []serviceAddress{}
		prefixes, err := AssignedPrefixes(app, overlay)
		if err != nil {
			return DNSRoutes{}, err
		}
		for _, p := range prefixes {
			rr := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: owner, Class: dnsmessage.ClassINET, Type: dnsmessage.TypeA}, Body: &dnsmessage.AResource{A: p.Addr().As4()}}
			r.services[name] = append(r.services[name], serviceAddress{rr, m.MapSignature.ExpiresAt})
		}
	}
	return r, nil
}

func (r DNSRoutes) serviceRecords(name string) ([]dnsmessage.Resource, bool) {
	var records []dnsmessage.Resource
	for _, h := range r.services[name] {
		if r.current != nil && r.current(h.until) {
			records = append(records, h.record)
		}
	}
	return records, len(records) > 0
}

func (r DNSRoutes) hasService(name string) bool {
	return r.services[foldDNSName(strings.TrimSuffix(name, "."))] != nil
}

// ExactServiceDomain identifies a resolver route that exists solely for one
// advertised service. Windows can install an exact NRPT rule for this name.
func (r DNSRoutes) ExactServiceDomain(name string) bool {
	_, covered := r.zones[r.match(name)]
	return !covered && r.services[name] != nil
}
