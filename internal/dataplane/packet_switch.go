package dataplane

import (
	"context"
	"encoding/binary"
	"net/netip"
	"sync"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wireguard-go/tun"
)

// packetBinding is the immutable plaintext delivery scope of a cipher. A
// signature renewal may retain it; changed identities, addresses or intents may
// not. It is not an application grant: Windows WFP remains the stateful ACL.
type packetBinding struct {
	account, network, localID, localKey, peerID, peerKey string
	local, peer                                          netip.Addr
	localGeneration, peerGeneration                      uint64
}

// packetSwitch is the sole native TUN reader. All engines, including primary
// WireGuard, read virtual ports. Closing an engine cannot close the shared TUN.
// The native session must remove dynamic permits before calling Close.
type packetSwitch struct {
	mu        sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	native    tun.Device
	primary   *packetPort
	lease     *connectivity.Lease
	maxPeers  int
	mapValue  connectivity.Map
	pairs     map[string]packetBinding
	byIP      map[netip.Addr]string
	routes    map[string]*peerRoute
	epoch     uint64
	closed    bool
	err       error
	closeErr  error
	done      chan struct{}
}

func newPacketSwitch(parent context.Context, native tun.Device, lease *connectivity.Lease, value connectivity.Map, mtu, maxPeers int) (*packetSwitch, error) {
	if parent == nil || parent.Err() != nil || native == nil || lease == nil || maxPeers <= 0 {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	s := &packetSwitch{ctx: ctx, cancel: cancel, native: native, lease: lease, maxPeers: maxPeers, routes: make(map[string]*peerRoute), done: make(chan struct{})}
	if err := s.Update(value); err != nil {
		cancel()
		return nil, err // native ownership stays with the caller on failure
	}
	port, err := newPacketPort(ctx, mtu, lease.Permissions, s.primaryIngress)
	if err != nil {
		cancel()
		return nil, err
	}
	s.primary = port
	go s.run()
	return s, nil
}

func packetBindings(value connectivity.Map, maxPeers int) (map[string]packetBinding, map[netip.Addr]string, error) {
	m, err := value.Snapshot()
	if err != nil || value.Denied() || len(m.Peers) > maxPeers {
		return nil, nil, ErrNoAuthority
	}
	local, err := netip.ParseAddr(m.Node.AssignedIP)
	if err != nil || !local.Is4() {
		return nil, nil, ErrConfiguration
	}
	pairs := make(map[string]packetBinding, len(m.Peers))
	byIP := make(map[netip.Addr]string, len(m.Peers))
	for _, peer := range m.Peers {
		if !pairGranted(peer) {
			continue
		}
		if peer.ID == "" || pairs[peer.ID].peerID != "" || len(peer.AllowedIPs) != 1 {
			return nil, nil, ErrConfiguration
		}
		prefix, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || prefix.Addr() == local || byIP[prefix.Addr()] != "" {
			return nil, nil, ErrConfiguration
		}
		b := packetBinding{account: m.Network.AccountID, network: m.Network.ID, localID: m.Node.ID, localKey: m.Node.PublicKey, peerID: peer.ID, peerKey: peer.PublicKey, local: local, peer: prefix.Addr(), localGeneration: value.IntentGeneration()}
		if peer.Transport != nil {
			b.peerGeneration = peer.Transport.PeerAdvertisement.IntentGeneration
		}
		pairs[peer.ID], byIP[b.peer] = b, peer.ID
	}
	return pairs, byIP, nil
}

// Update is called while the native source/lease pause is held. No old accepted
// map may send or deliver during the gap between Lease.Confirm and this update.
func (s *packetSwitch) Update(value connectivity.Map) error {
	if !s.lease.MatchesMap(value) || value.Denied() {
		return ErrNoAuthority
	}
	pairs, byIP, err := packetBindings(value, s.maxPeers)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var removed []*peerRoute
	for id, route := range s.routes {
		if previous, ok := s.pairs[id]; !ok || previous != pairs[id] {
			delete(s.routes, id)
			removed = append(removed, route)
		}
	}
	if value.Cursor().Revision != s.mapValue.Cursor().Revision {
		s.epoch++
	}
	s.mapValue, s.pairs, s.byIP = value, pairs, byIP
	s.mu.Unlock()
	for _, route := range removed {
		route.Invalidate()
	}
	return nil
}

func (s *packetSwitch) admittedLocked() bool {
	return !s.closed && s.ctx.Err() == nil && s.lease.MatchesMap(s.mapValue) && s.lease.Permissions().ExistingFlows
}

// SetRoute transfers only output choice, never resource ownership. The peer
// supervisor retains/tears down ciphers outside the packet switch's mutex.
func (s *packetSwitch) SetRoute(peer string, route *peerRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, exists := s.pairs[peer]
	if !exists || !s.admittedLocked() {
		return ErrNoAuthority
	}
	if route != nil {
		route.mu.Lock()
		r := route.relay
		closed := route.closed
		route.mu.Unlock()
		if closed || r == nil || r.bind.lease != s.lease {
			return ErrNoAuthority
		}
		r.bind.mu.Lock()
		local, remote, account, value := r.bind.local, r.bind.peer, r.bind.account, r.bind.mapValue
		r.bind.mu.Unlock()
		var peerGeneration uint64
		if remote.Transport != nil {
			peerGeneration = remote.Transport.PeerAdvertisement.IntentGeneration
		}
		if !s.lease.MatchesMap(value) || local.ID != b.localID || local.PublicKey != b.localKey || local.AssignedIP != b.local.String() || remote.ID != b.peerID || remote.PublicKey != b.peerKey || len(remote.AllowedIPs) != 1 || remote.AllowedIPs[0] != b.peer.String()+"/32" || account != b.account || r.bind.mux.transport.network != b.network || value.IntentGeneration() != b.localGeneration || peerGeneration != b.peerGeneration {
			return ErrNoAuthority
		}
		if s.routes[peer] == route {
			return nil
		}
		s.routes[peer] = route
	} else {
		delete(s.routes, peer)
	}
	s.epoch++
	return nil
}

func packetAddress(raw []byte, offset int) (netip.Addr, error) {
	if len(raw) < 20 || raw[0]>>4 != 4 || int(raw[0]&15)*4 < 20 || int(raw[0]&15)*4 > len(raw) || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) {
		return netip.Addr{}, ErrPacketIO
	}
	return netip.AddrFrom4([4]byte(raw[offset : offset+4])), nil
}

func (s *packetSwitch) primaryCurrent(b packetBinding, epoch uint64) bool {
	return s.routeCurrent(b, nil, epoch)
}

func (s *packetSwitch) routeCurrent(b packetBinding, route *peerRoute, epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admittedLocked() && s.epoch == epoch && s.pairs[b.peerID] == b && s.routes[b.peerID] == route
}

func (s *packetSwitch) send(raw []byte) error {
	destination, err := packetAddress(raw, 16)
	if err != nil {
		return err
	}
	s.mu.Lock()
	b, exists := s.pairs[s.byIP[destination]]
	route, epoch := s.routes[b.peerID], s.epoch
	allowed := exists && s.admittedLocked()
	s.mu.Unlock()
	if !allowed || applicationForPair(raw, b.local, b.peer) != nil {
		return ErrNoAuthority
	}
	if route != nil {
		// No fallback/retry of this same packet, including when the route has
		// temporarily lost all confirmed choices.
		return route.sendChecked(raw, func() bool { return s.routeCurrent(b, route, epoch) })
	}
	return s.primary.enqueueChecked(raw, false, func() bool { return s.primaryCurrent(b, epoch) })
}

func (s *packetSwitch) run() {
	defer close(s.done)
	slab := make([]byte, 65535+2*tun.ReadPacketSpacing)
	defer clear(slab)
	packets := make([]tun.ReadPacket, 1)
	for s.ctx.Err() == nil {
		n, err := s.native.Read(slab, packets)
		if n < 0 || n > len(packets) {
			err = ErrPacketIO
			n = 0
		}
		for i := 0; i < n; i++ {
			p := packets[i]
			if p.Offset < 0 || p.Size <= 0 || p.Offset > len(slab) || p.Size > len(slab)-p.Offset {
				err = ErrPacketIO
				break
			}
			_ = s.send(slab[p.Offset : p.Offset+p.Size])
			clear(slab[p.Offset : p.Offset+p.Size])
		}
		if err != nil || n == 0 {
			s.mu.Lock()
			if !s.closed && s.ctx.Err() == nil {
				s.err = ErrPacketIO
			}
			s.mu.Unlock()
			_ = s.primary.Close()
			return
		}
	}
}

func (s *packetSwitch) ingress(b packetBinding, raw []byte) error {
	if applicationForPair(raw, b.peer, b.local) != nil {
		return ErrNoAuthority
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	allowed := s.admittedLocked() && s.pairs[b.peerID] == b
	s.mu.Unlock()
	if !allowed {
		return ErrNoAuthority
	}
	// Every cipher has already applied AEAD/replay/source checks. This common
	// boundary checks current tuple/lease; native WFP classifies app flows.
	count, err := s.native.Write([][]byte{raw}, 0)
	if err != nil || count != 1 {
		return ErrPacketIO
	}
	s.mu.Lock()
	allowed = s.admittedLocked() && s.pairs[b.peerID] == b
	s.mu.Unlock()
	if !allowed {
		return ErrNoAuthority
	}
	return nil
}

func (s *packetSwitch) primaryIngress(raw []byte) error {
	source, err := packetAddress(raw, 12)
	if err != nil {
		return nil // consume malformed plaintext; never send it to native TUN
	}
	s.mu.Lock()
	b, exists := s.pairs[s.byIP[source]]
	s.mu.Unlock()
	if exists {
		_ = s.ingress(b, raw)
	}
	return nil
}

// Sink freezes the identity/IP/intent scope for an isolated peer cipher. It may
// accept qualified receive-only traffic independently of current output choice.
func (s *packetSwitch) Sink(value connectivity.Map, peer string) (func([]byte) error, error) {
	s.mu.Lock()
	b, exists := s.pairs[peer]
	allowed := exists && s.lease.MatchesMap(value) && value.Cursor() == s.mapValue.Cursor() && !s.closed
	s.mu.Unlock()
	if !allowed {
		return nil, ErrNoAuthority
	}
	return func(raw []byte) error { return s.ingress(b, raw) }, nil
}

func (s *packetSwitch) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *packetSwitch) outputChoices() map[string]peerRouteKind {
	s.mu.Lock()
	routes := make(map[string]*peerRoute, len(s.routes))
	for id, route := range s.routes {
		routes[id] = route
	}
	admitted := s.admittedLocked()
	s.mu.Unlock()
	result := make(map[string]peerRouteKind, len(routes))
	for id, route := range routes {
		if admitted {
			result[id] = route.Current()
		} else {
			result[id] = peerRouteNone
		}
	}
	return result
}

func (s *packetSwitch) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.epoch++
		s.mu.Unlock()
		s.cancel()
		_ = s.primary.Close()
		s.writeMu.Lock()
		s.closeErr = s.native.Close() // unblocks the one reader
		s.writeMu.Unlock()
		<-s.done
	})
	return s.closeErr
}
