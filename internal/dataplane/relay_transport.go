package dataplane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relay "github.com/endless-net/relay/api/wire"
)

var ErrRelay = errors.New("relay transport unavailable")
var ErrRelayPeer = errors.New("relay peer is not currently admitted")

type RelayConfig struct {
	DialTimeout, IOTimeout, HeartbeatInterval time.Duration
	Now                                       func() time.Time
	Roots                                     *x509.CertPool
}

func (c RelayConfig) Validate() error {
	if c.Now == nil || c.DialTimeout <= 0 || c.DialTimeout > 30*time.Second || c.IOTimeout <= 0 || c.IOTimeout > 30*time.Second || c.HeartbeatInterval%time.Millisecond != 0 || c.HeartbeatInterval < time.Duration(relay.MinHeartbeatIntervalMS)*time.Millisecond || c.HeartbeatInterval > time.Duration(relay.MaxHeartbeatIntervalMS)*time.Millisecond || c.IOTimeout <= c.HeartbeatInterval {
		return ErrConfiguration
	}
	return nil
}

type relaySocket interface {
	net.Conn
	ConnectionState() tls.ConnectionState
}

// RelayTransport carries only encrypted engine frames. TLS/Ready does not prove
// a peer path. The receiving engine must still validate AEAD/replay/source.
type RelayTransport struct {
	mu                     sync.Mutex
	config                 RelayConfig
	edge                   relay.Endpoint
	credential             relay.Credential
	network, node          string
	peers                  map[string]bool
	permission             permissionCheck
	scope                  func(connectivity.Map) bool
	handshakeMap           func() connectivity.Map
	mapValue               connectivity.Map
	lifetime               context.Context
	cancel                 context.CancelFunc
	done                   chan struct{}
	once                   sync.Once
	writeGate, readGate    chan struct{}
	socket                 relaySocket
	decoder                *relay.Decoder
	started, ready, closed bool
	dial                   func(context.Context, string, *tls.Config) (relaySocket, error)
}

func (*RelayTransport) String() string   { return "[encrypted Relay transport]" }
func (*RelayTransport) GoString() string { return "[encrypted Relay transport]" }

func NewRelayTransport(parent context.Context, m connectivity.Map, lease *connectivity.Lease, edgeID string, c RelayConfig) (*RelayTransport, error) {
	if parent == nil || parent.Err() != nil || lease == nil || !lease.MatchesMap(m) || m.Denied() || c.Validate() != nil {
		return nil, ErrConfiguration
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		return nil, ErrConfiguration
	}
	transport, err := newRelayTransport(parent, snapshot, lease.Permissions, edgeID, c)
	if err != nil {
		return nil, err
	}
	transport.scope = lease.MatchesMap
	transport.handshakeMap = lease.CurrentMap
	transport.mapValue = m
	return transport, nil
}

func newRelayTransport(parent context.Context, snapshot coordinator.NetworkMapSnapshot, permissions permissionCheck, edgeID string, c RelayConfig) (*RelayTransport, error) {
	if parent == nil || parent.Err() != nil || permissions == nil || c.Validate() != nil || snapshot.RelayCredential == nil || snapshot.Network.ID == "" || snapshot.Node.ID == "" || snapshot.RelayCredential.NetworkID != snapshot.Network.ID || snapshot.RelayCredential.NodeID != snapshot.Node.ID {
		return nil, ErrConfiguration
	}
	var edge relay.Endpoint
	for _, value := range snapshot.Relays {
		if value.ID == edgeID {
			edge = value
		}
	}
	host, _, err := net.SplitHostPort(edge.Addr)
	if edge.ID == "" || edge.Protocol != relay.EndpointProtocolTLS || err != nil || host == "" {
		return nil, ErrConfiguration
	}
	lifetime, cancel := context.WithCancel(parent)
	if c.Roots != nil {
		c.Roots = c.Roots.Clone()
	}
	t := &RelayTransport{config: c, edge: edge, credential: *snapshot.RelayCredential, network: snapshot.Network.ID, node: snapshot.Node.ID,
		peers: relayPeers(snapshot), permission: permissions, lifetime: lifetime, cancel: cancel, done: make(chan struct{}), writeGate: make(chan struct{}, 1), readGate: make(chan struct{}, 1)}
	t.dial = func(ctx context.Context, address string, config *tls.Config) (relaySocket, error) {
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: c.DialTimeout}, Config: config}
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, ErrRelay
		}
		secure, ok := connection.(*tls.Conn)
		if !ok {
			_ = connection.Close()
			return nil, ErrRelay
		}
		return secure, nil
	}
	context.AfterFunc(lifetime, func() { t.Close() })
	return t, nil
}

func relayPeers(snapshot coordinator.NetworkMapSnapshot) map[string]bool {
	peers := make(map[string]bool, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		if peer.NetworkID == snapshot.Network.ID && peer.ID != snapshot.Node.ID && peer.Status != "revoked" && len(peer.ACLGrants)+len(peer.IngressACLGrants) > 0 {
			peers[peer.ID] = true
		}
	}
	return peers
}

func (t *RelayTransport) permitted(newFlow bool) bool {
	t.mu.Lock()
	scope, m := t.scope, t.mapValue
	t.mu.Unlock()
	if scope != nil && !scope(m) {
		return false
	}
	p := t.permission()
	return !p.ClockUnsafe && !p.Revoked && p.ExistingFlows && (!newFlow || p.NewFlows) && t.config.Now().Before(t.credential.ExpiresAt)
}

func (t *RelayTransport) authorityGone() bool {
	p := t.permission()
	now := t.config.Now()
	return p.Revoked || p.ClockUnsafe || !now.Before(t.credential.ExpiresAt) || p.ExpiresAt.IsZero() || !now.Before(p.ExpiresAt)
}

func (t *RelayTransport) deadline(ctx context.Context) time.Time {
	expires := t.config.Now().Add(t.config.IOTimeout)
	if t.credential.ExpiresAt.Before(expires) {
		expires = t.credential.ExpiresAt
	}
	p := t.permission()
	if !p.ExpiresAt.IsZero() && p.ExpiresAt.Before(expires) {
		expires = p.ExpiresAt
	}
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	return expires
}

func (t *RelayTransport) Open(parent context.Context) (result error) {
	defer func() {
		if result != nil {
			t.Close()
		}
	}()
	t.mu.Lock()
	if t.started || t.closed {
		t.mu.Unlock()
		return ErrClosed
	}
	t.started = true
	t.mu.Unlock()
	if parent.Err() != nil {
		return parent.Err()
	}
	if !t.permitted(true) {
		return ErrNoAuthority
	}
	deadline := t.config.Now().Add(t.config.DialTimeout)
	if t.credential.ExpiresAt.Before(deadline) {
		deadline = t.credential.ExpiresAt
	}
	permission := t.permission()
	if !permission.ExpiresAt.IsZero() && permission.ExpiresAt.Before(deadline) {
		deadline = permission.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	stop := context.AfterFunc(t.lifetime, cancel)
	defer func() { stop(); cancel() }()
	host, _, err := net.SplitHostPort(t.edge.Addr)
	if err != nil {
		return ErrRelay
	}
	socket, err := t.dial(ctx, t.edge.Addr, &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: host, RootCAs: t.config.Roots})
	if err != nil || socket == nil {
		return ErrRelay
	}
	state := socket.ConnectionState()
	if ctx.Err() != nil || t.adoptHandshakeMap() != nil || !t.permitted(true) || !state.HandshakeComplete || state.Version != tls.VersionTLS13 || len(state.VerifiedChains) == 0 {
		_ = socket.Close()
		return ErrRelay
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = socket.Close()
		return ErrClosed
	}
	t.socket = socket
	t.decoder = relay.NewDecoder(socket)
	t.mu.Unlock()
	if err := t.write(ctx, &relay.ClientHello{Type: relay.MessageClientHello, ProtocolVersion: relay.Version, Credential: t.credential, HeartbeatIntervalMS: int(t.config.HeartbeatInterval / time.Millisecond)}, true); err != nil {
		t.Close()
		return err
	}
	message, err := t.read(ctx, true)
	ready, ok := message.(*relay.Ready)
	if err != nil || !ok || !ready.Ready || !t.permitted(true) {
		t.Close()
		return ErrRelay
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrClosed
	}
	t.ready = true
	t.mu.Unlock()
	go t.heartbeats()
	return nil
}

// Handshake I/O may span unrelated global revisions. Reauthorize this exact
// Relay endpoint from the latest confirmed sealed map without changing the
// original socket credential or dial context. Peer frame I/O never uses this
// adoption path and continues to wait for the native/supervisor map handoff.
func (t *RelayTransport) adoptHandshakeMap() error {
	if t.handshakeMap == nil {
		return nil
	}
	return t.Update(t.handshakeMap())
}

func (t *RelayTransport) acquire(ctx context.Context, gate chan struct{}) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return ErrClosed
	case gate <- struct{}{}:
		return nil
	}
}

func (t *RelayTransport) write(ctx context.Context, message relay.Message, newFlow bool) error {
	permitted := func() bool {
		if newFlow && t.adoptHandshakeMap() != nil {
			return false
		}
		if _, heartbeat := message.(*relay.Heartbeat); heartbeat {
			// A keepalive grants no peer traffic. Preserve the owned TLS socket
			// through temporary map/source pauses, under its original hard expiry.
			return !t.authorityGone()
		}
		return t.permitted(newFlow)
	}
	if err := t.acquire(ctx, t.writeGate); err != nil {
		return err
	}
	defer func() { <-t.writeGate }()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !permitted() {
		return ErrNoAuthority
	}
	if frame, ok := message.(*relay.ClientFrame); ok {
		t.mu.Lock()
		allowed := t.peers[frame.PeerID] && frame.PeerNetworkID == t.network
		t.mu.Unlock()
		if !allowed {
			return ErrRelayPeer
		}
	}
	raw, err := relay.MarshalMessage(message)
	if err != nil {
		return ErrRelay
	}
	t.mu.Lock()
	socket, closed := t.socket, t.closed
	t.mu.Unlock()
	if closed || socket == nil {
		return ErrClosed
	}
	if socket.SetWriteDeadline(t.deadline(ctx)) != nil {
		return ErrRelay
	}
	stop := context.AfterFunc(ctx, func() { t.Close() })
	defer stop()
	// Recheck after waiting for the writer and immediately before encrypted I/O.
	if !permitted() || ctx.Err() != nil {
		return ErrNoAuthority
	}
	if frame, ok := message.(*relay.ClientFrame); ok {
		t.mu.Lock()
		allowed := t.peers[frame.PeerID] && frame.PeerNetworkID == t.network
		t.mu.Unlock()
		if !allowed {
			return ErrRelayPeer
		}
	}
	n, err := socket.Write(raw)
	if err != nil || n != len(raw) {
		t.Close()
		return ErrRelay
	}
	if !permitted() {
		if t.authorityGone() {
			t.Close()
		}
		return ErrNoAuthority
	}
	return nil
}

func (t *RelayTransport) read(ctx context.Context, newFlow bool) (relay.Message, error) {
	if err := t.acquire(ctx, t.readGate); err != nil {
		return nil, err
	}
	defer func() { <-t.readGate }()
	if newFlow && t.adoptHandshakeMap() != nil {
		return nil, ErrNoAuthority
	}
	if !t.permitted(newFlow) {
		return nil, ErrNoAuthority
	}
	t.mu.Lock()
	socket, decoder, closed := t.socket, t.decoder, t.closed
	t.mu.Unlock()
	if closed || socket == nil || decoder == nil {
		return nil, ErrClosed
	}
	if socket.SetReadDeadline(t.deadline(ctx)) != nil {
		return nil, ErrRelay
	}
	stop := context.AfterFunc(ctx, func() { t.Close() })
	defer stop()
	message, err := decoder.Read()
	if err != nil || ctx.Err() != nil {
		t.Close()
		return nil, ErrRelay
	}
	if newFlow && t.adoptHandshakeMap() != nil {
		return nil, ErrNoAuthority
	}
	if !t.permitted(newFlow) {
		if t.authorityGone() {
			t.Close()
		}
		return nil, ErrNoAuthority
	}
	return message, nil
}

func (t *RelayTransport) Send(ctx context.Context, peer string, encrypted []byte) error {
	if len(encrypted) == 0 || len(encrypted) > relay.MaxFramePayloadBytes {
		return ErrPacketIO
	}
	t.mu.Lock()
	allowed, ready := t.peers[peer], t.ready && !t.closed
	t.mu.Unlock()
	if !allowed {
		return ErrRelayPeer
	}
	if !ready {
		return ErrRelay
	}
	return t.write(ctx, &relay.ClientFrame{Type: relay.MessageClientFrame, ProtocolVersion: relay.Version, PeerID: peer, PeerNetworkID: t.network, Payload: encrypted}, false)
}

func (t *RelayTransport) Receive(ctx context.Context) (string, []byte, error) {
	t.mu.Lock()
	ready := t.ready && !t.closed
	t.mu.Unlock()
	if !ready {
		return "", nil, ErrRelay
	}
	// Every frame has an idle deadline. Valid heartbeats keep an idle socket
	// alive; the caller deadline and original credential/lease expiry never move.
	for {
		message, err := t.read(ctx, false)
		if err != nil {
			return "", nil, err
		}
		switch value := message.(type) {
		case *relay.Heartbeat:
			continue // Never adopt a remote clock or grant.
		case *relay.ServerFrame:
			t.mu.Lock()
			allowed := t.peers[value.FromNodeID]
			t.mu.Unlock()
			if !allowed || value.FromNetworkID != t.network {
				t.Close()
				return "", nil, ErrRelayPeer
			}
			return value.FromNodeID, value.Payload, nil
		default:
			t.Close()
			return "", nil, ErrRelay
		}
	}
}

func (t *RelayTransport) Update(m connectivity.Map) error {
	if m.Denied() {
		t.Close()
		return ErrNoAuthority
	}
	if t.scope == nil || !t.scope(m) {
		return ErrNoAuthority
	}
	value, err := m.Snapshot()
	if err != nil || value.Network.ID != t.network || value.Node.ID != t.node {
		t.Close()
		return ErrNoAuthority
	}
	found := false
	for _, edge := range value.Relays {
		if edge.ID == t.edge.ID && edge.Addr == t.edge.Addr && edge.Protocol == t.edge.Protocol {
			found = true
		}
	}
	if !found {
		t.Close()
		return ErrRelay
	}
	t.mu.Lock()
	t.peers = relayPeers(value)
	t.mapValue = m
	t.mu.Unlock()
	// Updating a map never renews this socket's original bearer credential.
	return nil
}

func (t *RelayTransport) Ready() bool {
	return t.connected() && t.permitted(false)
}

func (t *RelayTransport) connected() bool {
	t.mu.Lock()
	ready := t.ready && !t.closed
	t.mu.Unlock()
	return ready
}

func (t *RelayTransport) heartbeats() {
	ticker := time.NewTicker(t.config.HeartbeatInterval)
	defer ticker.Stop()
	expiry := time.NewTimer(max(time.Duration(0), t.credential.ExpiresAt.Sub(t.config.Now())))
	defer expiry.Stop()
	for {
		select {
		case <-t.lifetime.Done():
			return
		case <-expiry.C:
			t.Close()
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(t.lifetime, t.config.IOTimeout)
			err := t.write(ctx, &relay.Heartbeat{Type: relay.MessageHeartbeat, ProtocolVersion: relay.Version, Time: t.config.Now().UTC().Format(time.RFC3339Nano)}, false)
			cancel()
			if err != nil {
				if errors.Is(err, ErrNoAuthority) && !t.authorityGone() {
					continue
				}
				t.Close()
				return
			}
		}
	}
}

func (t *RelayTransport) Close() {
	t.once.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.ready = false
		socket := t.socket
		t.mu.Unlock()

		t.cancel()
		if socket != nil {
			_ = socket.Close()
		}
		close(t.done)
	})
}
