package client

import (
	"net/netip"
	"reflect"
	"sync"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

type peerACLGrant struct {
	destination netip.Prefix
	ports       []clientapi.ACLPort
}

type peerACLRule struct {
	destination netip.Prefix
	deny        bool
	ports       []clientapi.ACLPort
	grants      []peerACLGrant
}

// Peer ACLs authorize outgoing overlay packets on every userspace platform.
// Rechecking every packet prevents an established flow from retaining a
// withdrawn grant. Replies remain subject to WireGuard source validation and
// the separate application/sharing policies.
type peerACLFilter struct {
	mu               sync.RWMutex
	current, pending []peerACLRule
	applying, closed bool
}

func compilePeerACL(peers []clientapi.Peer) []peerACLRule {
	var rules []peerACLRule
	// Use the same normalized destination/port intersections and longest-prefix
	// ordering as exported WireGuard configurations.
	for _, target := range aclFirewallTargets(peers) {
		rule := peerACLRule{destination: netip.MustParsePrefix(target.target), deny: target.deny, ports: target.ports}
		for _, grant := range target.grants {
			for _, destination := range grant.DestinationCIDRs {
				rule.grants = append(rule.grants, peerACLGrant{destination: netip.MustParsePrefix(destination), ports: grant.AllowedPorts})
			}
		}
		rules = append(rules, rule)
	}
	return rules
}

func (f *peerACLFilter) suspend(peers []clientapi.Peer) bool {
	rules := compilePeerACL(peers)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending, f.applying, f.closed = rules, true, false
	return !reflect.DeepEqual(f.current, rules)
}

func (f *peerACLFilter) commit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current, f.pending, f.applying, f.closed = f.pending, nil, false, false
}

func (f *peerACLFilter) withdraw() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *peerACLFilter) allows(raw []byte) bool {
	if f == nil {
		return true
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.closed {
		return false
	}
	// While configuration is changing, permit only the intersection. New
	// permissions become effective after the matching runtime configuration.
	return peerACLAllows(f.current, raw) && (!f.applying || peerACLAllows(f.pending, raw))
}

func peerACLAllows(rules []peerACLRule, raw []byte) bool {
	if len(rules) == 0 {
		return true
	}
	packet, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	for _, rule := range rules {
		if !rule.destination.Contains(packet.destination) {
			continue
		}
		if !rule.deny || peerACLPortAllows(rule.ports, packet) {
			return true
		}
		for _, grant := range rule.grants {
			if grant.destination.Contains(packet.destination) && (len(grant.ports) == 0 || peerACLPortAllows(grant.ports, packet)) {
				return true
			}
		}
		return false
	}
	return true
}

func peerACLPortAllows(ports []clientapi.ACLPort, packet applicationPacket) bool {
	for _, port := range ports {
		switch port.Protocol {
		case "tcp":
			if packet.protocol == 6 && int(packet.destinationPort) == port.Port {
				return true
			}
		case "udp":
			if packet.protocol == 17 && int(packet.destinationPort) == port.Port {
				return true
			}
		case "icmp":
			if packet.source.Is4() && packet.protocol == 1 || packet.source.Is6() && packet.protocol == 58 {
				return true
			}
		}
	}
	return false
}
