package client

import (
	"encoding/binary"
	"net/netip"
	"slices"
	"sync"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
)

type applicationGrant struct {
	source      netip.Addr
	destination netip.Prefix
	port        uint16
	expires     time.Time
	connector   bool
}

// Protected destinations survive withdrawal for the connected engine lifetime:
// deleting an application must not turn its traffic into unrestricted forwarding.
type applicationPacketFilter struct {
	mu            sync.RWMutex
	protected     map[netip.Prefix]bool
	grants        []applicationGrant
	expires       time.Time
	signed        bool
	saturated     bool
	connectorEver bool
	local         []netip.Addr
	generic       []netip.Prefix
}

func newApplicationPacketFilter() *applicationPacketFilter {
	return &applicationPacketFilter{protected: map[netip.Prefix]bool{}}
}

func (f *applicationPacketFilter) withdraw() { f.mu.Lock(); defer f.mu.Unlock(); f.grants = nil }

func (f *applicationPacketFilter) active() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.connectorEver || len(f.protected) > 0 || f.saturated
}

func (f *applicationPacketFilter) update(m clientapi.RegisterNodeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = nil
	f.expires = time.Time{}
	f.signed = m.MapSignature != nil
	if m.MapSignature != nil {
		f.expires = m.MapSignature.ExpiresAt
	}
	self := applicationSelf(m)
	f.local = nil
	f.generic = nil
	for _, value := range m.Node.AdvertisedIPs {
		if p, err := netip.ParsePrefix(value); err == nil {
			f.generic = append(f.generic, p)
		}
	}
	addresses := map[clientapi.ServiceHost][]netip.Addr{}
	for _, value := range []string{m.Node.AssignedIP, m.Node.AssignedIPv6} {
		if addr, err := netip.ParseAddr(value); err == nil {
			addresses[self] = append(addresses[self], addr)
		}
	}
	f.local = append(f.local, addresses[self]...)
	overlays := []netip.Prefix{}
	for _, value := range []string{m.Network.CIDR, m.Network.IPv6CIDR} {
		if p, err := netip.ParsePrefix(value); err == nil {
			overlays = append(overlays, p)
		}
	}
	for _, peer := range m.Peers {
		binding := clientapi.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
		for _, value := range peer.AllowedIPs {
			p, err := netip.ParsePrefix(value)
			if err != nil || p.Bits() != p.Addr().BitLen() {
				continue
			}
			for _, overlay := range overlays {
				if overlay.Contains(p.Addr()) {
					addresses[binding] = append(addresses[binding], p.Addr())
				}
			}
		}
	}
	for _, app := range m.Network.Applications {
		f.connectorEver = f.connectorEver || slices.Contains(app.Connectors, self)
		target, err := clientapi.ParseApplicationTarget(app.TargetType, app.Target)
		if err != nil {
			continue
		}
		for _, route := range app.Routes {
			for _, value := range route.CIDRs {
				p, err := netip.ParsePrefix(value)
				if err != nil {
					continue
				}
				if len(f.protected) < 8192 {
					f.protected[p] = true
				} else if !f.protected[p] {
					f.saturated = true
				}
				if route.Connector == self {
					for _, source := range app.Sources {
						for _, addr := range addresses[source] {
							f.grants = append(f.grants, applicationGrant{source: addr, destination: p, port: target.TCPPort, expires: route.ExpiresAt, connector: true})
						}
					}
				} else if slices.Contains(app.Sources, self) && !slices.Contains(app.Connectors, self) {
					for _, addr := range addresses[self] {
						f.grants = append(f.grants, applicationGrant{source: addr, destination: p, port: target.TCPPort, expires: route.ExpiresAt})
					}
				}
			}
		}
	}
}

type applicationPacket struct {
	source, destination         netip.Addr
	protocol                    byte
	sourcePort, destinationPort uint16
}

func parseApplicationPacket(raw []byte) (applicationPacket, bool) {
	var p applicationPacket
	if len(raw) < 1 {
		return p, false
	}
	var offset, end int
	switch raw[0] >> 4 {
	case 4:
		if len(raw) < 20 {
			return p, false
		}
		offset, end = int(raw[0]&15)*4, int(binary.BigEndian.Uint16(raw[2:4]))
		if offset < 20 || end < offset || end > len(raw) || binary.BigEndian.Uint16(raw[6:8])&0x3fff != 0 {
			return p, false
		}
		p.source = netip.AddrFrom4([4]byte(raw[12:16]))
		p.destination = netip.AddrFrom4([4]byte(raw[16:20]))
		p.protocol = raw[9]
	case 6:
		if len(raw) < 40 {
			return p, false
		}
		offset, end = 40, 40+int(binary.BigEndian.Uint16(raw[4:6]))
		if end > len(raw) {
			return p, false
		}
		p.source = netip.AddrFrom16([16]byte(raw[8:24]))
		p.destination = netip.AddrFrom16([16]byte(raw[24:40]))
		p.protocol = raw[6]
		// Fragment and extension headers are not authorized by this L3 parser.
		if p.protocol == 0 || p.protocol == 43 || p.protocol == 44 || p.protocol == 50 || p.protocol == 51 || p.protocol == 60 {
			return p, false
		}
	default:
		return p, false
	}
	if p.protocol == 6 || p.protocol == 17 {
		minimum := 8
		if p.protocol == 6 {
			minimum = 20
		}
		if end-offset < minimum {
			return p, false
		}
		p.sourcePort = binary.BigEndian.Uint16(raw[offset : offset+2])
		p.destinationPort = binary.BigEndian.Uint16(raw[offset+2 : offset+4])
	}
	return p, true
}

func (f *applicationPacketFilter) allows(raw []byte, inbound bool, now time.Time) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	// A map's authority expires for ordinary peers as well as application
	// destinations, even while control is unavailable or an update is stalled.
	if f.signed && !now.Before(f.expires) {
		return false
	}
	var source, destination netip.Addr
	switch {
	case len(raw) >= 20 && raw[0]>>4 == 4:
		source = netip.AddrFrom4([4]byte(raw[12:16]))
		destination = netip.AddrFrom4([4]byte(raw[16:20]))
	case len(raw) >= 40 && raw[0]>>4 == 6:
		source = netip.AddrFrom16([16]byte(raw[8:24]))
		destination = netip.AddrFrom16([16]byte(raw[24:40]))
	default:
		return false
	}
	protected := f.saturated
	forwarded := inbound && !slices.Contains(f.local, destination) || !inbound && !slices.Contains(f.local, source)
	if forwarded {
		generic := false
		if !f.connectorEver {
			for _, prefix := range f.generic {
				if inbound && prefix.Contains(destination) || !inbound && prefix.Contains(source) {
					generic = true
					break
				}
			}
		}
		if !generic {
			protected = true
		}
	}
	for prefix := range f.protected {
		if prefix.Contains(source) || prefix.Contains(destination) {
			protected = true
			break
		}
	}
	if !protected {
		return true
	}
	p, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	if !now.Before(f.expires) {
		return false
	}
	for _, grant := range f.grants {
		if !now.Before(grant.expires) {
			continue
		}
		request := inbound == grant.connector
		source, destination, port := p.source, p.destination, p.destinationPort
		if !request {
			source, destination, port = p.destination, p.source, p.sourcePort
		}
		if source == grant.source && grant.destination.Contains(destination) && (grant.port == 0 || p.protocol == 6 && port == grant.port) {
			return true
		}
	}
	return false
}

type applicationTUN struct {
	tun.Device
	filter  *applicationPacketFilter
	flows   *flowCollector
	sharing *sharingPacketFilter
	peerACL *peerACLFilter
}

func (t *applicationTUN) Write(bufs [][]byte, offset int) (int, error) {
	accepted := make([][]byte, 0, len(bufs))
	for _, buf := range bufs {
		if offset < 0 || len(buf) < offset {
			continue
		}
		now := time.Now()
		allowed := t.filter.allows(buf[offset:], true, now) && t.sharing.allows(buf[offset:], true, now)
		t.flows.observe(buf[offset:], allowed, now)
		if allowed {
			accepted = append(accepted, buf)
		}
	}
	if len(accepted) > 0 {
		if _, err := t.Device.Write(accepted, offset); err != nil {
			return 0, err
		}
	}
	return len(bufs), nil
}

func (t *applicationTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for {
		n, err := t.Device.Read(bufs, sizes, offset)
		if err != nil {
			return n, err
		}
		accepted := 0
		for i := 0; i < n; i++ {
			if offset < 0 || sizes[i] < 0 || offset+sizes[i] > len(bufs[i]) {
				continue
			}
			now := time.Now()
			packet := bufs[i][offset : offset+sizes[i]]
			normalizeIPv6UDPChecksum(packet)
			allowed := t.peerACL.allows(packet) && t.filter.allows(packet, false, now) && t.sharing.allows(packet, false, now)
			t.flows.observe(packet, allowed, now)
			if !allowed {
				continue
			}
			if accepted != i {
				copy(bufs[accepted][offset:], bufs[i][offset:offset+sizes[i]])
			}
			sizes[accepted] = sizes[i]
			accepted++
		}
		if accepted > 0 {
			return accepted, nil
		}
	}
}
