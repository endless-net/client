package dataplane

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

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
	source, destination netip.Addr
	protocol            string
	port                int
	egress              bool
}

type linuxFilterPlan struct {
	address   netip.Addr
	protected []netip.Addr
	grants    []linuxFlowGrant
}

func prepareLinuxFilter(snapshot coordinator.NetworkMapSnapshot, maxGrants, maxProtected int) (linuxFilterPlan, error) {
	local, err := netip.ParseAddr(snapshot.Node.AssignedIP)
	if err != nil || !local.Is4() || !local.IsGlobalUnicast() || maxGrants <= 0 || maxProtected <= 0 {
		return linuxFilterPlan{}, ErrConfiguration
	}
	plan := linuxFilterPlan{address: local, protected: []netip.Addr{local}}
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
				for _, port := range grant.AllowedPorts {
					if len(plan.grants) == maxGrants || !port.Valid() {
						return linuxFilterPlan{}, ErrConfiguration
					}
					plan.grants = append(plan.grants, linuxFlowGrant{
						source: direction.from, destination: direction.to,
						protocol: port.Protocol, port: int(port.Port), egress: direction.egress,
					})
				}
			}
		}
	}
	return plan, nil
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
	var rules strings.Builder
	table := "inet " + id.table
	if install {
		_, _ = fmt.Fprintf(&rules, "create table %s { comment %q; }\n", table, "endlessnet:"+id.owner)
		_, _ = fmt.Fprintf(&rules, "add set %s protected { type ipv4_addr; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add chain %s ingress_zone { type filter hook prerouting priority raw; policy accept; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add chain %s egress_zone { type filter hook output priority raw; policy accept; }\n", table)
		_, _ = fmt.Fprintf(&rules, "add rule %s ingress_zone iifname %q ct zone set %d\n", table, id.tunnel, id.zone)
		_, _ = fmt.Fprintf(&rules, "add rule %s egress_zone oifname %q ct zone set %d\n", table, id.tunnel, id.zone)
		for _, name := range []string{"input", "output", "forward"} {
			_, _ = fmt.Fprintf(&rules, "add chain %s %s { type filter hook %s priority -30; policy accept; }\n", table, name, name)
		}
	} else {
		for _, name := range []string{"input", "output", "forward"} {
			_, _ = fmt.Fprintf(&rules, "flush chain %s %s\n", table, name)
		}
		_, _ = fmt.Fprintf(&rules, "flush set %s protected\n", table)
	}
	addresses := append(slices.Clone(p.protected), retained...)
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
	if mode != linuxDeny {
		for _, grant := range p.grants {
			originalChain, originalInterface, replyChain, replyInterface := "input", "iifname", "output", "oifname"
			if grant.egress {
				originalChain, originalInterface, replyChain, replyInterface = replyChain, replyInterface, originalChain, originalInterface
			}
			forward := fmt.Sprintf("%s %s %q ip saddr %s ip daddr %s", originalChain, originalInterface, id.tunnel, grant.source, grant.destination)
			original := forward + " ct direction original"
			reply := fmt.Sprintf("%s %s %q ip saddr %s ip daddr %s ct direction reply", replyChain, replyInterface, id.tunnel, grant.destination, grant.source)
			if grant.protocol != coordinator.ProtocolAll {
				original += fmt.Sprintf(" %s dport %d", grant.protocol, grant.port)
				reply += fmt.Sprintf(" %s sport %d", grant.protocol, grant.port)
			} else if mode == linuxOnline {
				// Some IP protocols and ICMP errors have no independent tracked
				// flow. Only the currently authorized direction admits them.
				_, _ = fmt.Fprintf(&rules, "add rule %s %s ct state { invalid, untracked, related } accept\n", table, forward)
			}
			if mode == linuxOnline {
				newFlow := "ct state new"
				if grant.protocol == "tcp" {
					newFlow += " tcp flags & (syn | ack) == syn"
				}
				_, _ = fmt.Fprintf(&rules, "add rule %s %s %s ct mark set %d accept\n", table, original, newFlow, id.mark)
			}
			_, _ = fmt.Fprintf(&rules, "add rule %s %s ct state established ct mark %d accept\n", table, original, id.mark)
			_, _ = fmt.Fprintf(&rules, "add rule %s %s ct state established ct mark %d accept\n", table, reply, id.mark)
		}
	}
	for _, name := range []string{"input", "output", "forward"} {
		_, _ = fmt.Fprintf(&rules, "add rule %s %s ip saddr @protected drop\n", table, name)
		_, _ = fmt.Fprintf(&rules, "add rule %s %s ip daddr @protected drop\n", table, name)
	}
	_, _ = fmt.Fprintf(&rules, "add rule %s input iifname %q drop\n", table, id.tunnel)
	_, _ = fmt.Fprintf(&rules, "add rule %s output oifname %q drop\n", table, id.tunnel)
	_, _ = fmt.Fprintf(&rules, "add rule %s forward iifname %q drop\n", table, id.tunnel)
	_, _ = fmt.Fprintf(&rules, "add rule %s forward oifname %q drop\n", table, id.tunnel)
	return rules.String(), nil
}
