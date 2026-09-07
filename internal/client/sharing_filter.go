package client

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"sync"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

type sharingFlowKey struct {
	source, destination         netip.Addr
	sourcePort, destinationPort uint16
	protocol                    byte
}

type sharingFlow struct {
	binding    [32]byte
	until      time.Time
	leaseUntil time.Time
	tcpState   byte
}

type sharingPacketGrant struct {
	value       clientapi.SharePeerGrant
	binding     [32]byte
	localSource bool
}

// This filter runs on plaintext at the WireGuard TUN boundary, on both reads
// and writes. Sticky protected hosts prevent withdrawal from becoming allow-all.
// Every packet checks the lease, including established traffic without map I/O.
type sharingPacketFilter struct {
	mu        sync.Mutex
	protected map[netip.Addr]bool
	grants    []sharingPacketGrant
	flows     map[sharingFlowKey]sharingFlow
	saturated bool
}

func newSharingPacketFilter() *sharingPacketFilter {
	return &sharingPacketFilter{protected: map[netip.Addr]bool{}, flows: map[sharingFlowKey]sharingFlow{}}
}

func (f *sharingPacketFilter) active() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.protected) > 0 || f.saturated
}

func (f *sharingPacketFilter) withdraw() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = nil
	clear(f.flows)
}

// Suspend preserves eligible flow state but closes both existing and incoming
// protected hosts while the engine installs a new set of WireGuard identities.
func (f *sharingPacketFilter) suspend(m clientapi.RegisterNodeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = nil
	for _, grant := range m.Network.SharePeerGrants {
		remote := grant.SourceAllowedIPs
		if grant.SourceNodeID == m.Node.ID && grant.SourceNetworkID == m.Network.ID {
			remote = grant.RecipientAllowedIPs
		}
		for _, value := range remote {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				f.saturated = true
				continue
			}
			if len(f.protected) < 8192 {
				f.protected[prefix.Addr()] = true
			} else if !f.protected[prefix.Addr()] {
				f.saturated = true
			}
		}
	}
}

func (f *sharingPacketFilter) update(m clientapi.RegisterNodeResponse) {
	f.updateAt(m, time.Now())
}

func (f *sharingPacketFilter) updateAt(m clientapi.RegisterNodeResponse, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = nil
	if clientapi.ValidateSharePeerGrantsAt(m.Snapshot(), now) != nil {
		clear(f.flows)
		f.saturated = true
		return
	}
	bindings := map[[32]byte]time.Time{}
	for _, grant := range m.Network.SharePeerGrants {
		grant.Rights = append([]clientapi.ShareTraffic(nil), grant.Rights...)
		grant.SourceAllowedIPs = append([]string(nil), grant.SourceAllowedIPs...)
		grant.RecipientAllowedIPs = append([]string(nil), grant.RecipientAllowedIPs...)
		localSource := grant.SourceNodeID == m.Node.ID && grant.SourceNetworkID == m.Network.ID
		remoteIPs := grant.SourceAllowedIPs
		if localSource {
			remoteIPs = grant.RecipientAllowedIPs
		}
		for _, value := range remoteIPs {
			addr := netip.MustParsePrefix(value).Addr()
			if len(f.protected) < 8192 {
				f.protected[addr] = true
			} else if !f.protected[addr] {
				f.saturated = true
			}
		}
		identity := grant
		identity.IssuedAt, identity.ExpiresAt = time.Time{}, time.Time{}
		raw, err := json.Marshal(identity)
		if err != nil {
			f.saturated = true
			continue
		}
		binding := sha256.Sum256(raw)
		bindings[binding] = grant.ExpiresAt
		f.grants = append(f.grants, sharingPacketGrant{value: grant, binding: binding, localSource: localSource})
	}
	for key, flow := range f.flows {
		deadline, present := bindings[flow.binding]
		if !present || !now.Before(flow.leaseUntil) {
			delete(f.flows, key)
		} else {
			flow.leaseUntil = deadline
			f.flows[key] = flow
		}
	}
}

func sharingHasIP(values []string, addr netip.Addr) bool {
	for _, value := range values {
		if netip.MustParsePrefix(value).Addr() == addr {
			return true
		}
	}
	return false
}

func (f *sharingPacketFilter) allows(raw []byte, inbound bool, now time.Time) bool {
	if f == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saturated {
		return false
	}
	if len(f.protected) == 0 {
		return true
	}
	p, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	if !f.protected[p.source] && !f.protected[p.destination] {
		return true
	}
	offset := 40
	if raw[0]>>4 == 4 {
		offset = int(raw[0]&15) * 4
	}
	end := 40 + int(binary.BigEndian.Uint16(raw[4:6]))
	if raw[0]>>4 == 4 {
		end = int(binary.BigEndian.Uint16(raw[2:4]))
	}
	if p.protocol == 17 {
		length := int(binary.BigEndian.Uint16(raw[offset+4 : offset+6]))
		if length < 8 || offset+length > end {
			return false
		}
	}
	for _, grant := range f.grants {
		g := grant.value
		if now.Before(g.IssuedAt) || !now.Before(g.ExpiresAt) {
			continue
		}
		request := inbound == grant.localSource
		key := sharingFlowKey{p.source, p.destination, p.sourcePort, p.destinationPort, p.protocol}
		if !request {
			key.source, key.destination, key.sourcePort, key.destinationPort = key.destination, key.source, key.destinationPort, key.sourcePort
		}
		if !sharingHasIP(g.RecipientAllowedIPs, key.source) || !sharingHasIP(g.SourceAllowedIPs, key.destination) {
			continue
		}
		protocol := ""
		switch p.protocol {
		case 6:
			protocol = "tcp"
		case 17:
			protocol = "udp"
		case 1, 58:
			protocol = "icmp"
		}
		permitted := false
		for _, right := range g.Rights {
			if right.Protocol == protocol && (protocol == "icmp" || uint32(key.destinationPort) >= right.FirstPort && uint32(key.destinationPort) <= right.LastPort) {
				permitted = true
				break
			}
		}
		if !permitted {
			continue
		}
		if protocol == "icmp" {
			if end < offset+8 {
				return false
			}
			requestType, replyType := byte(8), byte(0)
			if p.protocol == 58 {
				requestType, replyType = 128, 129
			}
			if raw[offset+1] != 0 || request && raw[offset] != requestType || !request && raw[offset] != replyType {
				return false
			}
			key.sourcePort = binary.BigEndian.Uint16(raw[offset+4 : offset+6])
			key.destinationPort = binary.BigEndian.Uint16(raw[offset+6 : offset+8])
		}
		flow, exists := f.flows[key]
		exists = exists && flow.binding == grant.binding && now.Before(flow.until) && now.Before(flow.leaseUntil)
		if !exists {
			flow = sharingFlow{binding: grant.binding}
		}
		if protocol == "tcp" {
			headerLength := int(raw[offset+12]>>4) * 4
			if headerLength < 20 || offset+headerLength > end {
				return false
			}
			flags := raw[offset+13] & 0x3f
			if request && flags == 2 {
				if exists && flow.tcpState != 1 {
					return false
				}
				flow.tcpState = 1
			} else if !exists {
				continue
			} else if !request && flags == 18 {
				if flow.tcpState > 2 {
					return false
				}
				flow.tcpState = 2
			} else if flags&2 != 0 {
				return false
			} else if flags&4 == 0 {
				if flags&16 == 0 || request && flow.tcpState < 2 || !request && flow.tcpState < 3 {
					return false
				}
				flow.tcpState = 3
			}
		} else if !request && !exists {
			continue
		}
		if !exists && len(f.flows) >= 65536 {
			for oldKey, old := range f.flows {
				if !now.Before(old.until) {
					delete(f.flows, oldKey)
				}
			}
			if len(f.flows) >= 65536 {
				return false
			}
		}
		idle := 30 * time.Second
		if protocol == "tcp" {
			idle = 2 * time.Minute
		}
		flow.until = now.Add(idle)
		flow.leaseUntil = grant.value.ExpiresAt
		f.flows[key] = flow
		if protocol == "tcp" && raw[offset+13]&4 != 0 {
			delete(f.flows, key)
		}
		return true
	}
	return false
}
