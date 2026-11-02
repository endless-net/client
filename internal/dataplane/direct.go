package dataplane

import (
	"context"
	"encoding/base64"
	"errors"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
)

var ErrDirectPath = errors.New("direct path not confirmed")

// DirectPath is transport evidence, not application success or authorization.
// Authenticated means an inbound transport/keepalive passed the pinned engine's
// decryption and replay checks at the currently configured Direct endpoint.
type DirectPath struct {
	PeerID, PublicKey, Endpoint string
	Authenticated               bool
	ConfirmedAt                 time.Time
}

type directPeer struct {
	id, public string
	endpoint   netip.AddrPort
	token      uint64
}
type directConfig struct {
	peers     map[[32]byte]directPeer
	endpoints map[netip.AddrPort]bool
}
type directState struct {
	peer      directPeer
	session   device.PeerSessionState
	confirmed time.Time
	probed    bool
	current   bool
}

type directTracker struct {
	mu       sync.Mutex
	config   atomic.Pointer[directConfig]
	peers    map[[32]byte]directState
	sequence uint64
	changed  chan struct{}
	closed   bool
}

func newDirectTracker() *directTracker {
	t := &directTracker{peers: map[[32]byte]directState{}, changed: make(chan struct{})}
	t.config.Store(&directConfig{peers: map[[32]byte]directPeer{}, endpoints: map[netip.AddrPort]bool{}})
	return t
}
func (t *directTracker) notify() { close(t.changed); t.changed = make(chan struct{}) }

// configure runs before UAPI mutates peers. Callbacks never acquire Engine.mu or
// call Device, including callbacks delivered synchronously by removal/Close.
func (t *directTracker) configure(peers []coordinator.Peer) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrClosed
	}
	next := &directConfig{peers: make(map[[32]byte]directPeer, len(peers)), endpoints: make(map[netip.AddrPort]bool, len(peers))}
	states := make(map[[32]byte]directState, len(peers))
	for _, p := range peers {
		raw, err := base64.StdEncoding.Strict().DecodeString(p.PublicKey)
		if err != nil || len(raw) != 32 {
			return ErrConfiguration
		}
		endpoint, err := netip.ParseAddrPort(p.Endpoint)
		if err != nil {
			return ErrConfiguration
		}
		var key [32]byte
		copy(key[:], raw)
		old, exists := t.peers[key]
		binding := directPeer{id: p.ID, public: p.PublicKey, endpoint: endpoint}
		state := directState{peer: binding}
		if exists {
			state.session = old.session
			state.current = old.current
			if old.peer.endpoint == endpoint && old.peer.id == p.ID {
				state = old
				binding = old.peer
			}
		}
		if binding.token == 0 {
			if t.sequence == ^uint64(0) {
				return ErrEngine
			}
			t.sequence++
			binding.token = t.sequence
			state.peer = binding
		}
		next.peers[key], next.endpoints[endpoint], states[key] = binding, true, state
	}
	t.peers = states
	t.config.Store(next)
	t.notify()
	return nil
}

func (t *directTracker) session(key device.NoisePublicKey, state device.PeerSessionState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, exists := t.peers[[32]byte(key)]
	if !exists || t.closed {
		return
	}
	p.session = state
	if state != device.PeerSessionEstablished {
		p.confirmed = time.Time{}
	}
	if state == device.PeerSessionExpired || state == device.PeerSessionNone {
		p.probed = false
		p.current = false
	}
	t.peers[[32]byte(key)] = p
	t.notify()
}

// The priority-message hook is used only as a cheap notification that a usable
// CURRENT send keypair exists. Returning nil adds no message/protocol. Session
// Established alone can describe a responder's not-yet-confirmed NEXT keypair.
func (t *directTracker) currentKey(key device.NoisePublicKey) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, exists := t.peers[[32]byte(key)]
	if !exists || t.closed {
		return nil
	}
	p.current = true
	t.peers[[32]byte(key)] = p
	t.notify()
	return nil
}

func (t *directTracker) requestProbe(key [32]byte) (first, send bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, exists := t.peers[key]
	if !exists || t.closed {
		return false, false
	}
	if !p.probed {
		p.probed = true
		t.peers[key] = p
		return true, true
	}
	return false, p.current && p.session == device.PeerSessionEstablished
}

func (t *directTracker) authenticated(key [32]byte, endpoint netip.AddrPort, captured *directConfig, received time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, exists := t.peers[key]
	old, wasConfigured := captured.peers[key]
	if t.closed || !exists || !wasConfigured || old.token != p.peer.token || p.peer.endpoint != endpoint || p.session != device.PeerSessionEstablished {
		return
	}
	firstConfirmation := p.confirmed.IsZero()
	if received.After(p.confirmed) {
		p.confirmed = received
	}
	t.peers[key] = p
	if firstConfirmation {
		t.notify()
	}
}

func (t *directTracker) snapshot() ([]DirectPath, <-chan struct{}, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	paths := make([]DirectPath, 0, len(t.peers))
	for _, p := range t.peers {
		paths = append(paths, DirectPath{PeerID: p.peer.id, PublicKey: p.peer.public, Endpoint: p.peer.endpoint.String(), Authenticated: p.session == device.PeerSessionEstablished && !p.confirmed.IsZero(), ConfirmedAt: p.confirmed})
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].PublicKey < paths[j].PublicKey })
	return paths, t.changed, t.closed
}
func (t *directTracker) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		t.peers = nil
		t.config.Store(&directConfig{})
		t.notify()
	}
}

type directEndpoint struct {
	conn.Endpoint
	tracker  *directTracker
	config   *directConfig
	remote   netip.AddrPort
	received time.Time
}

func (e *directEndpoint) FromPeer(key [32]byte) {
	e.tracker.authenticated(key, e.remote, e.config, e.received)
}

// The endpoint wrapper is installed only by the receive path. FromPeer is called
// by wireguard-go after successful decryption AND replay validation, not on an
// arbitrary UDP datagram or a handshake initiation. Unknown/unannounced sources
// cannot move the pinned Direct endpoint via WireGuard's endpoint roaming.
type directBind struct {
	conn.Bind
	tracker *directTracker
}

func (b *directBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actual, err := b.Bind.Open(port)
	if err != nil {
		return fns, actual, err
	}
	for i, receive := range fns {
		if receive == nil {
			continue
		}
		fns[i] = func(slab []byte, packets []conn.ReceivedPacket) (int, error) {
			for {
				n, err := receive(slab, packets)
				if n < 0 || n > len(packets) {
					return 0, ErrPacketIO
				}
				config := b.tracker.config.Load()
				kept := 0
				for j := 0; j < n; j++ {
					p := packets[j]
					if p.Offset < 0 || p.Size < 0 || p.Offset > len(slab) || p.Size > len(slab)-p.Offset || p.Endpoint == nil {
						return 0, ErrPacketIO
					}
					remote, parseErr := netip.ParseAddrPort(p.Endpoint.DstToString())
					if parseErr != nil || !config.endpoints[remote] {
						continue
					}
					p.Endpoint = &directEndpoint{Endpoint: p.Endpoint, tracker: b.tracker, config: config, remote: remote, received: time.Now().UTC()}
					packets[kept] = p
					kept++
				}
				if kept != 0 || err != nil || n == 0 {
					return kept, err
				}
			}
		}
	}
	return fns, actual, nil
}

func (b *directBind) Send(bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	if endpoint == nil {
		return ErrDirectPath
	}
	if wrapped, ok := endpoint.(*directEndpoint); ok {
		endpoint = wrapped.Endpoint
	}
	remote, err := netip.ParseAddrPort(endpoint.DstToString())
	if err != nil || !b.tracker.config.Load().endpoints[remote] {
		return ErrDirectPath
	}
	return b.Bind.Send(bufs, endpoint, offset)
}

func (e *Engine) DirectPaths() ([]DirectPath, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrClosed
	}
	if e.bind == nil || !e.bind.permissions().ExistingFlows {
		return nil, ErrNoAuthority
	}
	paths, _, closed := e.direct.snapshot()
	if closed {
		return nil, ErrClosed
	}
	return paths, nil
}

// ProbeDirect requests standard empty WireGuard transport/keepalive packets; no
// application port, invented echo protocol or broad ACL exception is introduced.
// Upstream owns handshake/retry timing; the operation/Lease bounds every send.
func (e *Engine) ProbeDirect(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ctx == nil {
		return ErrConfiguration
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.closed {
		return ErrClosed
	}
	if e.bind == nil || !e.bind.permissions().NewFlows {
		return ErrNoAuthority
	}
	d, ok := e.device.(interface {
		LookupPeer(device.NoisePublicKey) *device.Peer
	})
	if !ok {
		return ErrEngine
	}
	config := e.direct.config.Load()
	for key := range config.peers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !e.bind.permissions().NewFlows {
			return ErrNoAuthority
		}
		peer := d.LookupPeer(device.NoisePublicKey(key))
		if peer == nil {
			return ErrEngine
		}
		first, send := e.direct.requestProbe(key)
		if !send {
			continue
		}
		if first {
			if err := peer.SendHandshakeInitiation(false); err != nil {
				return ErrEngine
			}
		}
		peer.SendKeepalive()
	}
	return nil
}

// WaitDirect observes fresh-engine transport evidence. The caller must still
// fence current map/intent/authority and original operation budget before success.
func (e *Engine) WaitDirect(ctx context.Context, probeInterval time.Duration) error {
	if ctx == nil || probeInterval < time.Millisecond || probeInterval > 30*time.Second {
		return ErrConfiguration
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrConfiguration
	}
	probe := time.NewTicker(probeInterval)
	defer probe.Stop()
	if err := e.ProbeDirect(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return ErrClosed
		}
		paths, changed, closed := e.direct.snapshot()
		_, sourceChanged := e.lease.SourceSignal()
		permitted := e.bind != nil && e.bind.permissions().NewFlows
		e.mu.Unlock()
		if closed {
			return ErrClosed
		}
		if !permitted {
			return ErrNoAuthority
		}
		for _, p := range paths {
			if p.Authenticated {
				return e.ProbeDirect(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
			if err := e.ProbeDirect(ctx); err != nil {
				return err
			}
		case <-sourceChanged:
		case <-probe.C:
			if err := e.ProbeDirect(ctx); err != nil {
				return err
			}
		}
	}
}
