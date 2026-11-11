package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"sync"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relay "github.com/endless-net/relay/api/wire"
)

// relayMux has one frame reader per TLS connection. Ciphertext is demultiplexed
// into bounded per-peer queues; the engine must authenticate it independently.
type relayMux struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	transport *RelayTransport
	maxPeers  int
	peers     map[string]*relayPeerBind
	wake      chan struct{}
	done      chan struct{}
	closed    bool
}

func newRelayMux(parent context.Context, t *RelayTransport, maxPeers int) (*relayMux, error) {
	if parent == nil || parent.Err() != nil || t == nil || !t.Ready() || maxPeers < 1 {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	m := &relayMux{ctx: ctx, cancel: cancel, transport: t, maxPeers: maxPeers, peers: make(map[string]*relayPeerBind), wake: make(chan struct{}, 1), done: make(chan struct{})}
	context.AfterFunc(ctx, m.Close)
	go m.run()
	return m, nil
}

func (m *relayMux) run() {
	defer close(m.done)
	for {
		peer, raw, err := m.transport.Receive(m.ctx)
		if err != nil {
			if errors.Is(err, ErrNoAuthority) && !m.transport.authorityGone() {
				select {
				case <-m.ctx.Done():
					return
				case <-m.transport.done:
					m.Close()
					return
				case <-m.wake:
					continue
				}
			}
			m.Close()
			return
		}
		m.mu.Lock()
		bind := m.peers[peer]
		m.mu.Unlock()
		if bind != nil {
			bind.enqueue(raw)
		}
	}
}

func (m *relayMux) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	peers := make([]*relayPeerBind, 0, len(m.peers))
	for _, bind := range m.peers {
		peers = append(peers, bind)
	}
	m.mu.Unlock()
	m.cancel()
	m.transport.Close()
	for _, bind := range peers {
		bind.shutdown()
	}
}

func (m *relayMux) Update(value connectivity.Map) error {
	if err := m.transport.Update(value); err != nil {
		if m.transport.authorityGone() || !m.transport.connected() {
			m.Close()
		}
		return err
	}
	m.mu.Lock()
	peers := make([]*relayPeerBind, 0, len(m.peers))
	for _, b := range m.peers {
		peers = append(peers, b)
	}
	m.mu.Unlock()
	for _, b := range peers {
		if err := b.update(value); err != nil {
			if !b.lease.MatchesMap(value) {
				return ErrNoAuthority
			}
			b.shutdown()
		}
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// New bindings require fresh authority. Existing ones may carry only already
// authorized flows while offline; packetPort/native stateful ACL classify those.
func (m *relayMux) newBind(parent context.Context, value connectivity.Map, lease *connectivity.Lease, peerID string) (*relayPeerBind, error) {
	if parent == nil || parent.Err() != nil || lease == nil || value.Denied() || !lease.MatchesMap(value) || !lease.Permissions().NewFlows {
		return nil, ErrNoAuthority
	}
	snapshot, err := value.Snapshot()
	if err != nil || snapshot.Network.ID != m.transport.network || snapshot.Node.ID != m.transport.node {
		return nil, ErrNoAuthority
	}
	var selected coordinator.Peer
	for _, peer := range snapshot.Peers {
		if peer.ID == peerID && pairGranted(peer) {
			selected = peer
			break
		}
	}
	if selected.ID == "" {
		return nil, ErrRelayPeer
	}
	// Logical cookie/routing endpoint only. This bind never opens a UDP socket
	// or publishes its port as a Direct endpoint.
	endpoint, err := netip.ParseAddrPort(selected.Endpoint)
	if err != nil {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	b := &relayPeerBind{mux: m, ctx: ctx, cancel: cancel, lease: lease, mapValue: value, revisionFloor: 1, scopeEpoch: 1, peer: selected, endpoint: endpoint, local: snapshot.Node, account: snapshot.Network.AccountID}
	m.mu.Lock()
	if m.closed || len(m.peers) >= m.maxPeers || m.peers[peerID] != nil {
		m.mu.Unlock()
		cancel()
		return nil, ErrConfiguration
	}
	m.peers[peerID] = b
	m.mu.Unlock()
	context.AfterFunc(ctx, b.shutdown)
	return b, nil
}

type relayPeerQueue struct {
	mu     sync.Mutex
	frames chan []byte
	done   chan struct{}
	bytes  int
	closed bool
}

func newRelayPeerQueue() *relayPeerQueue {
	return &relayPeerQueue{frames: make(chan []byte, portQueueFrames), done: make(chan struct{})}
}
func (q *relayPeerQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.done)
	for {
		select {
		case raw := <-q.frames:
			q.bytes -= len(raw)
			clear(raw)
		default:
			return
		}
	}
}

type relayPeerBind struct {
	mu            sync.Mutex
	mux           *relayMux
	ctx           context.Context
	cancel        context.CancelFunc
	lease         *connectivity.Lease
	mapValue      connectivity.Map
	local         coordinator.Node
	account       string
	peer          coordinator.Peer
	endpoint      netip.AddrPort
	queue         *relayPeerQueue
	closed        bool
	revisionFloor uint64
	scopeEpoch    uint64
}

func (*relayPeerBind) BatchSize() int { return 1 }
func (*relayPeerBind) SetMark(mark uint32) error {
	if mark != 0 {
		return ErrConfiguration
	}
	return nil
}
func (b *relayPeerBind) permissions() connectivity.Permission {
	b.mu.Lock()
	lease, m, closed := b.lease, b.mapValue, b.closed
	b.mu.Unlock()
	if closed || b.ctx.Err() != nil || !lease.MatchesMap(m) {
		return connectivity.Permission{}
	}
	return lease.Permissions()
}
func (b *relayPeerBind) update(value connectivity.Map) error {
	if value.Denied() || !b.lease.MatchesMap(value) {
		return ErrNoAuthority
	}
	snapshot, err := value.Snapshot()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || snapshot.Network.AccountID != b.account || snapshot.Network.ID != b.mux.transport.network || snapshot.Node.ID != b.local.ID || snapshot.Node.PublicKey != b.local.PublicKey || snapshot.Node.AssignedIP != b.local.AssignedIP {
		return ErrNoAuthority
	}
	for _, peer := range snapshot.Peers {
		if peer.ID == b.peer.ID && peer.PublicKey == b.peer.PublicKey && pairGranted(peer) {
			if peer.Transport != nil && b.peer.Transport != nil && peer.Transport.PeerAdvertisement.IntentGeneration != b.peer.Transport.PeerAdvertisement.IntentGeneration {
				return ErrNoAuthority
			}
			if len(peer.AllowedIPs) != 1 || len(b.peer.AllowedIPs) != 1 || peer.AllowedIPs[0] != b.peer.AllowedIPs[0] {
				return ErrConfiguration
			}
			// Primary Direct endpoint changes do not affect this virtual Relay channel.
			if snapshot.Node.Status != b.local.Status || snapshot.Node.ApprovalState != b.local.ApprovalState || peer.Status != b.peer.Status || peer.ACLRestricted != b.peer.ACLRestricted || !reflect.DeepEqual(peer.ACLGrants, b.peer.ACLGrants) || !reflect.DeepEqual(peer.IngressACLGrants, b.peer.IngressACLGrants) {
				b.revisionFloor = value.Cursor().Revision.Network
				b.scopeEpoch++
			}
			b.peer = peer
			b.local = snapshot.Node
			b.mapValue = value
			return nil
		}
	}
	return ErrRelayPeer
}
func (b *relayPeerBind) ParseEndpoint(value string) (awgconn.Endpoint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if value != b.endpoint.String() {
		return nil, ErrConfiguration
	}
	return &awgconn.StdNetEndpoint{AddrPort: b.endpoint}, nil
}
func (b *relayPeerBind) Open(uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return nil, 0, net.ErrClosed
	}
	if b.queue != nil {
		return nil, 0, awgconn.ErrBindAlreadyOpen
	}
	queue := newRelayPeerQueue()
	// Opening a bounded logical receiver is not transmission or a peer proof.
	// A map handoff during crypto construction must not destroy this device;
	// Send/enqueue/receive and the plaintext sink retain current authority gates.
	b.queue = queue
	// One is an internal logical port required by the engine Bind interface;
	// it acknowledges no network listener and must never be advertised.
	return []awgconn.ReceiveFunc{func(packets [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
		return b.receive(queue, packets, sizes, endpoints)
	}}, 1, nil
}
func (b *relayPeerBind) Close() error {
	b.mu.Lock()
	queue := b.queue
	b.queue = nil
	b.mu.Unlock()
	if queue != nil {
		queue.close()
	}
	return nil
}
func (b *relayPeerBind) shutdown() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	id := b.peer.ID
	b.mu.Unlock()
	b.cancel()
	_ = b.Close()
	b.mux.mu.Lock()
	if b.mux.peers[id] == b {
		delete(b.mux.peers, id)
	}
	b.mux.mu.Unlock()
}
func (b *relayPeerBind) Send(packets [][]byte, endpoint awgconn.Endpoint) error {
	if len(packets) != 1 || len(packets[0]) == 0 || len(packets[0]) > relay.MaxFramePayloadBytes || endpoint == nil {
		return ErrPacketIO
	}
	b.mu.Lock()
	allowed := !b.closed && b.queue != nil && endpoint.DstToString() == b.endpoint.String()
	peer := b.peer.ID
	b.mu.Unlock()
	if !allowed {
		return ErrRelayPeer
	}
	if !b.permissions().ExistingFlows {
		return ErrNoAuthority
	}
	err := b.mux.transport.Send(b.ctx, peer, packets[0])
	if err != nil {
		return err
	}
	if !b.permissions().ExistingFlows {
		return ErrNoAuthority
	}
	return nil
}
func (b *relayPeerBind) enqueue(raw []byte) {
	if len(raw) == 0 || len(raw) > relay.MaxFramePayloadBytes || !b.permissions().ExistingFlows {
		return
	}
	b.mu.Lock()
	q := b.queue
	b.mu.Unlock()
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(q.frames) == cap(q.frames) || len(raw) > transport.MaxProbeBytes-q.bytes {
		return
	}
	copyRaw := append([]byte(nil), raw...)
	q.frames <- copyRaw
	q.bytes += len(copyRaw)
}
func (b *relayPeerBind) receive(q *relayPeerQueue, packets [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
	if len(packets) != 1 || len(sizes) != 1 || len(endpoints) != 1 {
		return 0, ErrPacketIO
	}
	select {
	case <-b.ctx.Done():
		return 0, net.ErrClosed
	case <-q.done:
		return 0, net.ErrClosed
	case raw := <-q.frames:
		q.mu.Lock()
		q.bytes -= len(raw)
		closed := q.closed
		q.mu.Unlock()
		defer clear(raw)
		if closed {
			return 0, net.ErrClosed
		}
		if !b.permissions().ExistingFlows {
			return 0, nil
		}
		if len(raw) > len(packets[0]) {
			return 0, ErrPacketIO
		}
		b.mu.Lock()
		endpoint := b.endpoint
		current := b.queue == q
		b.mu.Unlock()
		if !current {
			return 0, net.ErrClosed
		}
		copy(packets[0], raw)
		sizes[0] = len(raw)
		endpoints[0] = &awgconn.StdNetEndpoint{AddrPort: endpoint}
		if !b.permissions().ExistingFlows {
			clear(packets[0][:len(raw)])
			return 0, nil
		}
		return 1, nil
	}
}
