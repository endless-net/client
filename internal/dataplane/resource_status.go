package dataplane

import (
	"net/netip"
	"slices"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	coord "github.com/endless-net/coordinator/api/enrollment"
)

// Only recipient assignments are disclosed. The packet switch publishes a
// selected rule after the native peer assignment has completed.
func (s *packetSwitch) observedResources(value connectivity.Map, m coord.NetworkMapSnapshot) []*clientv0.Resource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.admittedLocked() || s.mapValue.Cursor() != value.Cursor() {
		return nil
	}
	local := coord.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	rows := make([]*clientv0.Resource, 0, len(m.Network.Applications))
	for _, app := range m.Network.Applications {
		if slices.Contains(app.Connectors, local) {
			continue
		}
		r := &clientv0.Resource{Id: app.ID, NetworkId: m.Network.ID, DisplayName: app.Name}
		if r.DisplayName == "" {
			r.DisplayName = app.Target
		}
		var prefixes []netip.Prefix
		switch app.TargetType {
		case "ip":
			address, err := netip.ParseAddr(app.Target)
			if err != nil {
				continue
			}
			prefixes = []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}
			r.Kind = clientv0.ResourceKind_RESOURCE_KIND_HOST
			r.Target = &clientv0.Resource_Host{Host: &clientv0.HostTarget{Addresses: []string{app.Target}}}
		case "cidr":
			p, err := netip.ParsePrefix(app.Target)
			if err != nil {
				continue
			}
			prefixes = []netip.Prefix{p}
			r.Kind = clientv0.ResourceKind_RESOURCE_KIND_SUBNET
			r.Target = &clientv0.Resource_Subnet{Subnet: &clientv0.SubnetTarget{Cidr: app.Target}}
		case "domain":
			r.Kind = clientv0.ResourceKind_RESOURCE_KIND_HOST
			r.Target = &clientv0.Resource_Host{Host: &clientv0.HostTarget{Hostname: app.Target}}
			overlay, _ := netip.ParsePrefix(m.Network.CIDR)
			prefixes, _ = connectivity.AssignedPrefixes(app, overlay)
			for _, p := range prefixes {
				r.GetHost().Addresses = append(r.GetHost().Addresses, p.Addr().String())
			}
		default:
			continue
		}
		active := slices.ContainsFunc(prefixes, func(p netip.Prefix) bool {
			rule := s.resources[p]
			return rule.peer != "" && slices.ContainsFunc(app.Routes, func(path coord.ApplicationRoute) bool {
				return path.Connector.NodeID == rule.peer && slices.Contains(path.CIDRs, p.String())
			}) && s.lease.GrantDeadlineCurrent(value, rule.end)
		})
		r.Availability = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "client.resource.no_path"}
		if active {
			r.Availability = &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}
		}
		r.Enabled = &clientv0.BooleanSetting{Effective: true, Control: &clientv0.SettingControl{Source: clientv0.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY, Mutation: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_UNSUPPORTED}}}
		rows = append(rows, r)
	}
	return rows
}
