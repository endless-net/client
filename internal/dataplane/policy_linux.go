package dataplane

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/underlay"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
)

type linuxFilterIdentity struct {
	table, tunnel string
	owner         string
	zone          uint16
	mark          uint32
}

var linuxObjectName = regexp.MustCompile(`^en[a-f0-9]{12}$`)

type linuxFlowGrant struct {
	source      netip.Addr
	destination netip.Prefix
	protocol    string
	port        int
	egress      bool
	validUntil  time.Time
}

type linuxTransitGrant struct {
	linuxFlowGrant
	masquerade bool
}

type linuxFilterPlan struct {
	probes          []linuxProbeBinding
	address         netip.Addr
	protected       []netip.Addr
	grants          []linuxFlowGrant
	direct          map[netip.Addr]string
	overlay         netip.Prefix
	transit         map[netip.Prefix][]linuxTransitGrant
	transitCount    int
	deadlineAdvance time.Duration
	unavailable     []netip.Prefix
	activeResources []netip.Prefix
}

func prepareLinuxFilter(snapshot coordinator.NetworkMapSnapshot, maxGrants, maxProtected int) (linuxFilterPlan, error) {
	local, err := netip.ParseAddr(snapshot.Node.AssignedIP)
	if err != nil || !local.Is4() || !local.IsGlobalUnicast() || maxGrants <= 0 || maxProtected <= 0 {
		return linuxFilterPlan{}, ErrConfiguration
	}
	plan := linuxFilterPlan{address: local, protected: []netip.Addr{local}}
	if err := plan.recipientResources(snapshot, maxGrants, maxProtected); err != nil {
		return linuxFilterPlan{}, err
	}
	seen := map[netip.Addr]bool{local: true}
	for _, peer := range snapshot.Peers {
		if len(peer.AllowedIPs) != 1 || !peer.ACLRestricted || len(peer.AllowedPorts) != 0 {
			return linuxFilterPlan{}, ErrConfiguration
		}
		remote, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || remote.Bits() != 32 || !remote.Addr().Is4() || !remote.Addr().IsGlobalUnicast() ||
			remote.String() != peer.AllowedIPs[0] || seen[remote.Addr()] || len(plan.protected) == maxProtected {
			return linuxFilterPlan{}, ErrConfiguration
		}
		seen[remote.Addr()] = true
		plan.protected = append(plan.protected, remote.Addr())
		for _, direction := range []struct {
			from, to netip.Addr
			grants   []coordinator.ACLGrant
			egress   bool
		}{{local, remote.Addr(), peer.ACLGrants, true}, {remote.Addr(), local, peer.IngressACLGrants, false}} {
			for _, grant := range direction.grants {
				if len(grant.DestinationCIDRs) != 1 || grant.DestinationCIDRs[0] != direction.to.String()+"/32" || len(grant.AllowedPorts) == 0 {
					return linuxFilterPlan{}, ErrConfiguration
				}
				var until time.Time
				if grant.ValidUntil != nil {
					until = *grant.ValidUntil
					if until.Unix() <= 0 || uint64(until.Unix()) > ^uint64(0)/1_000_000_000 {
						return linuxFilterPlan{}, ErrConfiguration
					}
				}
				for _, port := range grant.AllowedPorts {
					if len(plan.grants)+plan.transitCount >= maxGrants || !port.Valid() {
						return linuxFilterPlan{}, ErrConfiguration
					}
					plan.grants = append(plan.grants, linuxFlowGrant{
						source: direction.from, destination: netip.PrefixFrom(direction.to, 32),
						protocol: port.Protocol, port: int(port.Port), egress: direction.egress, validUntil: until,
					})
				}
			}
		}
	}
	if len(plan.protected)+len(plan.activeResources)+len(plan.unavailable)+len(plan.transit) > maxProtected {
		return linuxFilterPlan{}, ErrConfiguration
	}
	return plan, nil
}

// A recipient may send only from its overlay address, to the signed target
// and ports, through the tunnel. Keep every target protected on other links.
func (p *linuxFilterPlan) recipientResources(snapshot coordinator.NetworkMapSnapshot, maxGrants, maxProtected int) error {
	if len(snapshot.Network.Applications) > maxProtected {
		return ErrConfiguration
	}
	paths, err := mapResourcePaths(snapshot)
	if err != nil {
		return err
	}
	selected := make(map[string][]connectivity.ResourcePath)
	for _, path := range paths {
		selected[path.ApplicationID] = append(selected[path.ApplicationID], path)
	}
	inactive := snapshot
	inactive.Network.Applications = nil
	peers := make(map[string]coordinator.Peer, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		peers[peer.ID] = peer
	}
	local := coordinator.ServiceHost{NodeID: snapshot.Node.ID, PublicKey: snapshot.Node.PublicKey}
	overlay, _ := netip.ParsePrefix(snapshot.Network.CIDR)
	p.overlay = overlay
	p.transit = make(map[netip.Prefix][]linuxTransitGrant)
	for _, app := range snapshot.Network.Applications {
		if connectivity.LocalDomainDiscovery(app, local) {
			continue
		}
		if slices.Contains(app.Connectors, local) {
			prefixes, err := connectivity.AssignedPrefixes(app, overlay)
			if err != nil {
				return ErrConfiguration
			}
			// A withdrawn router assignment still shadows broader transit
			// grants. Preserve the router's own ordinary LAN access.
			for _, prefix := range prefixes {
				if _, ok := p.transit[prefix]; !ok {
					p.transit[prefix] = nil
				}
			}
		}
		if len(app.Routes) == 0 {
			inactive.Network.Applications = append(inactive.Network.Applications, app)
			continue
		}

		until := snapshot.MapSignature.ExpiresAt
		if !until.After(snapshot.MapSignature.IssuedAt) || until.Unix() <= 0 || uint64(until.Unix()) > ^uint64(0)/1_000_000_000 {
			return ErrConfiguration
		}
		if slices.Contains(app.Connectors, local) {
			if err := p.addTransit(snapshot, app, local, peers, maxGrants); err != nil {
				return err
			}
			continue
		}
		if len(selected[app.ID]) == 0 || len(app.Sources) != 0 {
			return ErrConfiguration
		}
		for _, path := range selected[app.ID] {
			p.activeResources = append(p.activeResources, path.Prefix)
			for _, port := range app.AllowedPorts {
				if len(p.grants)+p.transitCount >= maxGrants {
					return ErrConfiguration
				}
				p.grants = append(p.grants, linuxFlowGrant{source: p.address, destination: path.Prefix, protocol: port.Protocol, port: int(port.Port), egress: true, validUntil: until})
			}
		}
	}
	p.unavailable, err = connectivity.UnavailableExitPrefixes(inactive, maxProtected)
	if err != nil {
		return ErrConfiguration
	}
	for _, prefix := range p.activeResources {
		if _, ok := p.transit[prefix]; ok {
			return ErrConfiguration
		}
	}
	for _, prefix := range p.unavailable {
		if _, ok := p.transit[prefix]; ok {
			return ErrConfiguration
		}
	}
	slices.SortFunc(p.activeResources, netip.Prefix.Compare)
	p.activeResources = slices.Compact(p.activeResources)
	return nil
}

// mapResourcePaths already validated the shape of every route and connector.
// Only the local router's own lease authorizes forwarding from mapped source keys.
func (p *linuxFilterPlan) addTransit(snapshot coordinator.NetworkMapSnapshot, app coordinator.Application, local coordinator.ServiceHost, peers map[string]coordinator.Peer, maxGrants int) error {
	return visitRouterGrants(snapshot, app, local, peers, func(prefix netip.Prefix, _ string, source netip.Addr, port coordinator.ACLPort, masquerade bool, until time.Time) error {
		if len(p.grants)+p.transitCount >= maxGrants {
			return ErrConfiguration
		}
		grant := linuxFlowGrant{source: source, destination: prefix, protocol: port.Protocol, port: port.Port, validUntil: until}
		for _, other := range p.transit[prefix] {
			if other.source == source && other.masquerade != masquerade && (other.protocol == coordinator.ProtocolAll || grant.protocol == coordinator.ProtocolAll || other.protocol == grant.protocol && other.port == grant.port) {
				return ErrConfiguration
			}
		}
		p.transit[prefix] = append(p.transit[prefix], linuxTransitGrant{grant, masquerade})
		p.transitCount++
		return nil
	})
}

type linuxFilterMode uint8

const (
	linuxDeny linuxFilterMode = iota
	linuxExisting
	linuxOnline
)

// transaction installs a new policy or replaces its chains atomically. Packet
// marks are minted only on the owned tunnel in its conntrack zone. Established
// traffic must still match the currently signed directional grant.
func (p linuxFilterPlan) transaction(id linuxFilterIdentity, install bool, mode linuxFilterMode, retained []netip.Addr) (string, error) {
	owner, err := uuid.Parse(id.owner)
	if !linuxObjectName.MatchString(id.table) || !linuxObjectName.MatchString(id.tunnel) ||
		err != nil || owner == uuid.Nil || owner.String() != id.owner ||
		id.zone == 0 || id.mark == 0 || mode > linuxOnline || !p.address.Is4() {
		return "", ErrConfiguration
	}
	prefix, group := linuxPeerScope(id.owner)
	interfaces := make(map[string]bool, len(p.direct))
	for peer, name := range p.direct {
		if !peer.Is4() || peer == p.address || !slices.Contains(p.protected, peer) ||
			!linuxPeerLinkName.MatchString(name) || !strings.HasPrefix(name, prefix) || interfaces[name] {
			return "", ErrConfiguration
		}
		interfaces[name] = true
	}
	// The group and session-specific name prefix are assigned atomically at
	// creation. Even an unselected peer link is covered by the terminal drops.
	kernelInput := fmt.Sprintf("iifgroup %d iifname %q", group, prefix+"*")
	kernelOutput := fmt.Sprintf("oifgroup %d oifname %q", group, prefix+"*")
	var rules strings.Builder
	table := "inet " + id.table
	if install {
		_, _ = fmt.Fprintf(&rules, "create table %s { comment %q; }\n", table, "endlessnet:"+id.owner)
		_, _ = fmt.Fprintf(&rules, "add set %s protected { type ipv4_addr; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add chain %s ingress_zone { type filter hook prerouting priority raw; policy accept; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add chain %s egress_zone { type filter hook output priority raw; policy accept; }\n", table)
		fmt.Fprintf(&rules, "add chain %s resource_nat { type nat hook postrouting priority srcnat; policy accept; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add rule %s egress_zone oifname %q ct zone set %d\n", table, id.tunnel, id.zone)
		fmt.Fprintf(&rules, "add rule %s egress_zone %s ct zone set %d\n", table, kernelOutput, id.zone)
		for _, name := range []string{"input", "output", "forward"} {
			_, _ = fmt.Fprintf(&rules, "add chain %s %s { type filter hook %s priority -30; policy accept; }\n", table, name, name)
		}
		for _, chain := range []string{"input", "output"} {
			for _, verdict := range []string{"accept", "drop"} {
				name := chain + "_" + verdict
				fmt.Fprintf(&rules, "add chain %s %s\nadd rule %s %s %s\n", table, name, table, name, verdict)
			}
		}
	} else {
		for _, name := range []string{"input", "output", "forward", "ingress_zone", "resource_nat"} {
			_, _ = fmt.Fprintf(&rules, "flush chain %s %s\n", table, name)
		}
		_, _ = fmt.Fprintf(&rules, "flush set %s protected\n", table)
	}
	addresses := append(slices.Clone(p.protected), retained...)
	occupied := make(map[netip.Addr]bool, len(p.protected)+2*len(p.probes))
	for _, address := range p.protected {
		occupied[address] = true
	}
	for _, probe := range p.probes {
		if !linuxPeerLinkName.MatchString(probe.device) || !strings.HasPrefix(probe.device, prefix) ||
			probe.port == 0 || probe.local == probe.peer || !probe.local.Is4() || !probe.local.IsGlobalUnicast() ||
			!probe.peer.Is4() || !probe.peer.IsGlobalUnicast() || occupied[probe.local] || occupied[probe.peer] {
			return "", ErrConfiguration
		}
		occupied[probe.local], occupied[probe.peer] = true, true
		addresses = append(addresses, probe.local, probe.peer)
		if mode == linuxOnline {
			rules.WriteString(probe.rules(table, group, id.mark))
		}
	}
	slices.SortFunc(addresses, netip.Addr.Compare)
	addresses = slices.Compact(addresses)
	elements := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if !address.Is4() {
			return "", ErrConfiguration
		}
		elements = append(elements, address.String())
	}
	if len(elements) == 0 {
		return "", ErrConfiguration
	}
	_, _ = fmt.Fprintf(&rules, "add element %s protected { %s }\n", table, strings.Join(elements, ", "))
	// WireGuard chooses the longest matching prefix. A broader resource
	// must not supply additional ports for the more specific router.
	resources := make(map[netip.Prefix][]linuxFlowGrant, len(p.activeResources)+len(p.unavailable))
	for prefix := range p.transit {
		resources[prefix] = nil
	}
	for _, prefix := range p.activeResources {
		resources[prefix] = nil
	}
	for _, grant := range p.grants {
		if _, ok := resources[grant.destination]; ok {
			resources[grant.destination] = append(resources[grant.destination], grant)
		} else if mode != linuxDeny {
			p.writeFlowGrant(&rules, id, table, mode, grant)
		}
	}
	for _, prefix := range p.unavailable {
		resources[prefix] = nil
	}
	prefixes := make([]netip.Prefix, 0, len(resources))
	for prefix := range resources {
		prefixes = append(prefixes, prefix)
	}
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		if a.Bits() != b.Bits() {
			return b.Bits() - a.Bits()
		}
		return a.Compare(b)
	})
	for _, prefix := range prefixes {
		if transit, ok := p.transit[prefix]; ok {
			// Physical replies arrive in the default conntrack zone, including
			// replies to SNAT. Keep the outgoing transit leg in that zone too.
			fmt.Fprintf(&rules, "add rule %s ingress_zone iifname %q ip daddr %s%s return\n", table, id.tunnel, prefix, p.exitFilter(prefix, "daddr"))
			if mode != linuxDeny && !slices.Contains(p.unavailable, prefix) {
				for _, grant := range transit {
					p.writeTransitGrant(&rules, id, table, mode, grant)
				}
			}
			fmt.Fprintf(&rules, "add rule %s forward iifname %q ip daddr %s drop\n", table, id.tunnel, prefix)
			fmt.Fprintf(&rules, "add rule %s forward oifname %q ip saddr %s drop\n", table, id.tunnel, prefix)
			fmt.Fprintf(&rules, "add rule %s resource_nat iifname %q ip daddr %s%s return\n", table, id.tunnel, prefix, p.exitFilter(prefix, "daddr"))
			continue
		}
		if mode != linuxDeny {
			for _, grant := range resources[prefix] {
				p.writeFlowGrant(&rules, id, table, mode, grant)
			}
		}
		dst := prefix.String() + p.exitFilter(prefix, "daddr")
		src := prefix.String() + p.exitFilter(prefix, "saddr")
		if prefix.Bits() == 0 {
			// Leave marked outer packets to the host's remaining policy.
			// More specific resource guards ran first. The final tunnel and
			// protected-address guards below still apply to marked packets.
			fmt.Fprintf(&rules, "add rule %s output ip daddr %s meta mark != %#x goto output_drop\n", table, dst, underlay.Mark)
		} else {
			fmt.Fprintf(&rules, "add rule %s output ip daddr %s goto output_drop\n", table, dst)
		}
		fmt.Fprintf(&rules, "add rule %s input iifname %q ip saddr %s goto input_drop\n", table, id.tunnel, src)
		if prefix.Bits() == 0 {
			// A recipient's exit controls its own traffic. Other physical
			// interfaces may forward independently of the tunnel.
			fmt.Fprintf(&rules, "add rule %s forward iifname %q ip daddr %s drop\n", table, id.tunnel, dst)
		} else {
			fmt.Fprintf(&rules, "add rule %s forward ip daddr %s drop\n", table, dst)
		}
		fmt.Fprintf(&rules, "add rule %s forward oifname %q ip saddr %s drop\n", table, id.tunnel, src)
		fmt.Fprintf(&rules, "add rule %s resource_nat iifname %q ip daddr %s%s return\n", table, id.tunnel, prefix, p.exitFilter(prefix, "daddr"))
	}
	fmt.Fprintf(&rules, "add rule %s ingress_zone %s ct zone set %d\n", table, kernelInput, id.zone)
	fmt.Fprintf(&rules, "add rule %s ingress_zone iifname %q ct zone set %d\n", table, id.tunnel, id.zone)
	for _, name := range []string{"input", "output", "forward"} {
		verdict := "drop"
		if name != "forward" {
			verdict = "goto " + name + "_drop"
		}
		fmt.Fprintf(&rules, "add rule %s %s ip saddr @protected %s\n", table, name, verdict)
		fmt.Fprintf(&rules, "add rule %s %s ip daddr @protected %s\n", table, name, verdict)
	}
	fmt.Fprintf(&rules, "add rule %s input iifname %q goto input_drop\n", table, id.tunnel)
	fmt.Fprintf(&rules, "add rule %s output oifname %q goto output_drop\n", table, id.tunnel)
	_, _ = fmt.Fprintf(&rules, "add rule %s forward iifname %q drop\n", table, id.tunnel)
	_, _ = fmt.Fprintf(&rules, "add rule %s forward oifname %q drop\n", table, id.tunnel)
	fmt.Fprintf(&rules, "add rule %s input %s drop\n", table, kernelInput)
	fmt.Fprintf(&rules, "add rule %s output %s drop\n", table, kernelOutput)
	fmt.Fprintf(&rules, "add rule %s forward %s drop\n", table, kernelInput)
	fmt.Fprintf(&rules, "add rule %s forward %s drop\n", table, kernelOutput)
	return rules.String(), nil
}

func (p linuxFilterPlan) writeFlowGrant(rules *strings.Builder, id linuxFilterIdentity, table string, mode linuxFilterMode, grant linuxFlowGrant) {
	originalChain, originalInterface, replyChain, replyInterface := "input", "iifname", "output", "oifname"
	if grant.egress {
		originalChain, originalInterface, replyChain, replyInterface = replyChain, replyInterface, originalChain, originalInterface
	}
	destination := grant.destination.String()
	if grant.destination.Bits() == 32 {
		destination = grant.destination.Addr().String()
	}
	peer := grant.source
	if grant.egress {
		peer = grant.destination.Addr()
	}
	in := fmt.Sprintf("%s %q", originalInterface, id.tunnel)
	out := fmt.Sprintf("%s %q", replyInterface, id.tunnel)
	if name, ok := p.direct[peer]; ok {
		_, group := linuxPeerScope(id.owner)
		in = fmt.Sprintf("%sgroup %d %s %q", strings.TrimSuffix(originalInterface, "name"), group, originalInterface, name)
		out = fmt.Sprintf("%sgroup %d %s %q", strings.TrimSuffix(replyInterface, "name"), group, replyInterface, name)
	}
	forward := fmt.Sprintf("%s %s ip saddr %s ip daddr %s", originalChain, in, grant.source, destination)
	reply := fmt.Sprintf("%s %s ip saddr %s ip daddr %s ct direction reply", replyChain, out, destination, grant.source)
	forward += p.exitFilter(grant.destination, "daddr")
	reply += p.exitFilter(grant.destination, "saddr")
	if !grant.validUntil.IsZero() {
		// nft takes whole Unix seconds. Round down and omit any
		// expired deadline outside its unsigned representation.
		until := grant.validUntil.Add(-p.deadlineAdvance).Unix()
		if until <= 0 {
			return
		}
		bound := fmt.Sprintf(" meta time < %d", until)
		forward += bound
		reply += bound
	}
	original := forward + " ct direction original"
	if grant.protocol != coordinator.ProtocolAll {
		original += fmt.Sprintf(" %s dport %d", grant.protocol, grant.port)
		reply += fmt.Sprintf(" %s sport %d", grant.protocol, grant.port)
	} else if mode == linuxOnline {
		// Some IP protocols and ICMP errors have no independent tracked
		// flow. Only the currently authorized direction admits them.
		_, _ = fmt.Fprintf(rules, "add rule %s %s ct state { invalid, untracked, related } goto %s_accept\n", table, forward, originalChain)
	}
	if mode == linuxOnline {
		newFlow := "ct state new"
		if grant.protocol == "tcp" {
			newFlow += " tcp flags & (syn | ack) == syn"
		}
		_, _ = fmt.Fprintf(rules, "add rule %s %s %s ct mark set %d goto %s_accept\n", table, original, newFlow, id.mark, originalChain)
	}
	_, _ = fmt.Fprintf(rules, "add rule %s %s ct state established ct mark %d goto %s_accept\n", table, original, id.mark, originalChain)
	_, _ = fmt.Fprintf(rules, "add rule %s %s ct state established ct mark %d goto %s_accept\n", table, reply, id.mark, replyChain)
}

func (p linuxFilterPlan) exitFilter(target netip.Prefix, field string) string {
	if target.Bits() != 0 {
		return ""
	}
	var filter strings.Builder
	for _, prefix := range exitLocalNetworks {
		fmt.Fprintf(&filter, " ip %s != %s", field, prefix)
	}
	fmt.Fprintf(&filter, " ip %s != %s", field, p.overlay)
	return filter.String()
}

func (p linuxFilterPlan) writeTransitGrant(rules *strings.Builder, id linuxFilterIdentity, table string, mode linuxFilterMode, g linuxTransitGrant) {
	until := g.validUntil.Add(-p.deadlineAdvance).Unix()
	if until <= 0 {
		return
	}
	forward := fmt.Sprintf("iifname %q oifname != %q ip saddr %s ip daddr %s meta time < %d ct direction original", id.tunnel, id.tunnel, g.source, g.destination, until)
	reply := fmt.Sprintf("iifname != %q oifname %q ip saddr %s ip daddr %s meta time < %d ct direction reply", id.tunnel, id.tunnel, g.destination, g.source, until)
	forward += p.exitFilter(g.destination, "daddr")
	reply += p.exitFilter(g.destination, "saddr")
	if g.protocol != coordinator.ProtocolAll {
		forward += fmt.Sprintf(" %s dport %d", g.protocol, g.port)
		reply += fmt.Sprintf(" %s sport %d", g.protocol, g.port)
	}
	if mode == linuxOnline {
		newFlow := "ct state new"
		if g.protocol == "tcp" {
			newFlow += " tcp flags & (syn | ack) == syn"
		}
		fmt.Fprintf(rules, "add rule %s forward %s %s ct mark set %d accept\n", table, forward, newFlow, id.mark)
		if g.masquerade {
			fmt.Fprintf(rules, "add rule %s resource_nat %s ct mark %d masquerade\n", table, forward, id.mark)
		}
	}
	status := "ct status & snat == 0"
	if g.masquerade {
		status = "ct status snat"
	}
	for _, dir := range []string{forward, reply} {
		fmt.Fprintf(rules, "add rule %s forward %s ct state established ct mark %d %s accept\n", table, dir, id.mark, status)
	}
}
