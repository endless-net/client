package dataplane

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relay "github.com/endless-net/relay/api/wire"
)

// Synthetic I/O exercises the protocol boundary; it is not TLS/peer evidence.
type relaySocketFixture struct {
	mu                          sync.Mutex
	input                       *bytes.Reader
	chunk                       int
	output                      bytes.Buffer
	state                       tls.ConnectionState
	closed                      bool
	beforeRead                  func()
	readDeadline, writeDeadline time.Time
}

func (s *relaySocketFixture) Read(raw []byte) (int, error) {
	if s.beforeRead != nil {
		s.beforeRead()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	if s.chunk > 0 && len(raw) > s.chunk {
		raw = raw[:s.chunk]
	}
	return s.input.Read(raw)
}
func (s *relaySocketFixture) Write(raw []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	return s.output.Write(raw)
}
func (s *relaySocketFixture) Close() error       { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }
func (*relaySocketFixture) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (*relaySocketFixture) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (s *relaySocketFixture) SetDeadline(v time.Time) error {
	_ = s.SetReadDeadline(v)
	return s.SetWriteDeadline(v)
}
func (s *relaySocketFixture) SetReadDeadline(v time.Time) error {
	s.mu.Lock()
	s.readDeadline = v
	s.mu.Unlock()
	return nil
}
func (s *relaySocketFixture) SetWriteDeadline(v time.Time) error {
	s.mu.Lock()
	s.writeDeadline = v
	s.mu.Unlock()
	return nil
}
func (s *relaySocketFixture) ConnectionState() tls.ConnectionState { return s.state }

func relayTransportFixture(t *testing.T, frames ...relay.Message) (*RelayTransport, *relaySocketFixture, *packetPermission) {
	t.Helper()
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	snapshot := coordinator.NetworkMapSnapshot{Network: coordinator.Network{ID: "network"}, Node: coordinator.Node{ID: "node"},
		Relays:          []relay.Endpoint{{ID: "edge", Addr: "relay.example:443", Protocol: relay.EndpointProtocolTLS}},
		RelayCredential: &relay.Credential{Algorithm: relay.CredentialAlgorithm, KeyID: "ed25519:" + base64RelayKey(), NetworkID: "network", NodeID: "node", ExpiresAt: expires, Signature: base64RelaySignature()},
		Peers:           []coordinator.Peer{{ID: "peer", NetworkID: "network", ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}}}}}}
	p := &packetPermission{p: connectivity.Permission{NewFlows: true, ExistingFlows: true, ExpiresAt: expires}}
	var input bytes.Buffer
	ready, _ := relay.MarshalMessage(&relay.Ready{Type: relay.MessageReady, ProtocolVersion: relay.Version, Ready: true})
	input.Write(ready)
	for _, frame := range frames {
		raw, err := relay.MarshalMessage(frame)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(raw)
	}
	socket := &relaySocketFixture{input: bytes.NewReader(input.Bytes()), chunk: len(ready), state: tls.ConnectionState{Version: tls.VersionTLS13, HandshakeComplete: true, VerifiedChains: [][]*x509.Certificate{{{}}}}}
	transport, err := newRelayTransport(t.Context(), snapshot, p.get, "edge", RelayConfig{Now: time.Now, DialTimeout: time.Second, IOTimeout: 2 * time.Second, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	transport.dial = func(ctx context.Context, address string, config *tls.Config) (relaySocket, error) {
		if deadline, ok := ctx.Deadline(); !ok || deadline.After(expires) || address != "relay.example:443" || config.ServerName != "relay.example" || config.MinVersion != tls.VersionTLS13 || config.MaxVersion != tls.VersionTLS13 || config.InsecureSkipVerify {
			t.Fatal("TLS identity/version/budget bypass")
		}
		return socket, nil
	}
	t.Cleanup(transport.Close)
	return transport, socket, p
}

func openingRelayMapFixture(t *testing.T) (*RelayTransport, *relaySocketFixture, *candidateFixture) {
	t.Helper()
	f := newCandidateFixture(t)
	f.withRelay(t)
	transport, err := NewRelayTransport(t.Context(), f.verified, f.lease, "edge", RelayConfig{Now: func() time.Time { s, _ := f.clock.Sample(); return s.Wall }, DialTimeout: time.Second, IOTimeout: 2 * time.Second, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := relay.MarshalMessage(&relay.Ready{Type: relay.MessageReady, ProtocolVersion: relay.Version, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	socket := &relaySocketFixture{input: bytes.NewReader(raw), chunk: len(raw), state: tls.ConnectionState{Version: tls.VersionTLS13, HandshakeComplete: true, VerifiedChains: [][]*x509.Certificate{{{}}}}}
	transport.dial = func(context.Context, string, *tls.Config) (relaySocket, error) { return socket, nil }
	t.Cleanup(transport.Close)
	return transport, socket, f
}

func TestRelayOpenReauthorizesLatestMapAfterDialAndReadyWithoutRenewingDeadline(t *testing.T) {
	transport, socket, f := openingRelayMapFixture(t)
	originalCredential := transport.credential
	bump := func() {
		cursor := f.verified.Cursor()
		f.clock.advance(100*time.Millisecond, 100*time.Millisecond)
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision = f.snapshot.Revision.Network
		f.refresh(t, &cursor)
	}
	var deadline time.Time
	transport.dial = func(ctx context.Context, _ string, _ *tls.Config) (relaySocket, error) {
		deadline, _ = ctx.Deadline()
		bump()
		return socket, nil
	}
	var once sync.Once
	socket.beforeRead = func() { once.Do(bump) }
	if err := transport.Open(t.Context()); err != nil {
		t.Fatal("unrelated revision during TLS/Ready prevented Open", err)
	}
	if !transport.Ready() || transport.mapValue.Cursor() != f.verified.Cursor() || transport.credential != originalCredential {
		t.Fatal("Open did not adopt current endpoint authority or renewed its bearer")
	}
	if deadline.IsZero() || socket.readDeadline.After(deadline) || socket.writeDeadline.After(deadline) {
		t.Fatal("new map extended original dial/Hello/Ready deadline")
	}
	bump()
	if transport.Send(t.Context(), "peer", []byte{1}) == nil || transport.mapValue.Cursor() == f.verified.Cursor() {
		t.Fatal("handshake adoption bypassed the explicit peer-frame map handoff")
	}
}

func TestRelayOpenRejectsChangedEndpointOrHardFenceBeforeSendingCredential(t *testing.T) {
	for _, action := range []string{"endpoint", "revoke", "pause", "clock", "old-credential"} {
		t.Run(action, func(t *testing.T) {
			transport, socket, f := openingRelayMapFixture(t)
			transport.dial = func(context.Context, string, *tls.Config) (relaySocket, error) {
				switch action {
				case "endpoint":
					cursor := f.verified.Cursor()
					f.clock.advance(time.Millisecond, time.Millisecond)
					f.snapshot.Revision.Network++
					f.snapshot.Network.Revision = f.snapshot.Revision.Network
					f.snapshot.Relays[0].Addr = "other-relay.example:443"
					f.refresh(t, &cursor)
				case "revoke":
					f.lease.Revoke()
				case "pause":
					f.lease.PausePackets(true)
				case "clock":
					f.clock.mu.Lock()
					f.clock.err = connectivity.ErrClock
					f.clock.mu.Unlock()
				case "old-credential":
					f.clock.advance(31*time.Second, 31*time.Second)
					sample, _ := f.clock.Sample()
					endpointExpiry := sample.Wall.Add(time.Minute)
					f.snapshot.Node.EndpointExpiresAt = &endpointExpiry
					f.snapshot.Peers[0].EndpointExpiresAt = &endpointExpiry
					f.withRelay(t) // Fresh map/bearer must not renew the original socket.
				}
				return socket, nil
			}
			if transport.Open(t.Context()) == nil || transport.Ready() {
				t.Fatal("changed endpoint or hard source fence admitted Open")
			}
			socket.mu.Lock()
			defer socket.mu.Unlock()
			if !socket.closed || socket.output.Len() != 0 {
				t.Fatal("failed endpoint adoption leaked socket or sent a credential")
			}
		})
	}
}

func TestRelayHeartbeatPreservesSocketDuringSoftFenceWithoutAdmittingPeerData(t *testing.T) {
	transport, socket, permission := relayTransportFixture(t)
	if err := transport.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	permission.mu.Lock()
	permission.p.NewFlows, permission.p.ExistingFlows = false, false
	permission.mu.Unlock()
	heartbeat := &relay.Heartbeat{Type: relay.MessageHeartbeat, ProtocolVersion: relay.Version, Time: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := transport.write(t.Context(), heartbeat, false); err != nil {
		t.Fatal("temporary source fence stopped the bounded keepalive", err)
	}
	if err := transport.Send(t.Context(), "peer", []byte{1}); err == nil {
		t.Fatal("keepalive admission authorized peer data")
	}
	socket.mu.Lock()
	raw := bytes.Clone(socket.output.Bytes())
	socket.mu.Unlock()
	decoder := relay.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Read(); err != nil {
		t.Fatal(err)
	}
	if message, err := decoder.Read(); err != nil {
		t.Fatal(err)
	} else if _, ok := message.(*relay.Heartbeat); !ok {
		t.Fatal("soft fence transmitted peer data")
	}
	permission.mu.Lock()
	permission.p.Revoked = true
	permission.mu.Unlock()
	if err := transport.write(t.Context(), heartbeat, false); err == nil {
		t.Fatal("hard revocation allowed keepalive")
	}
}

func base64RelayKey() string { return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" }
func base64RelaySignature() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}

func TestRelayTransportUsesProducerFramesWithoutClaimingPeerReachability(t *testing.T) {
	transport, socket, _ := relayTransportFixture(t, &relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version, FromNodeID: "peer", FromNetworkID: "network", Payload: []byte{1, 2, 3}})
	if transport.Ready() {
		t.Fatal("unopened Relay reported ready")
	}
	if err := transport.Open(t.Context()); err != nil || !transport.Ready() {
		t.Fatal("Relay Ready negotiation failed", err)
	}
	if err := transport.Send(t.Context(), "peer", []byte{4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	peer, payload, err := transport.Receive(t.Context())
	if err != nil || peer != "peer" || !bytes.Equal(payload, []byte{1, 2, 3}) {
		t.Fatal("producer frame binding lost", err)
	}
	socket.mu.Lock()
	output := append([]byte(nil), socket.output.Bytes()...)
	writeDeadline := socket.writeDeadline
	socket.mu.Unlock()
	decoder := relay.NewDecoder(bytes.NewReader(output))
	hello, err := decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := hello.(*relay.ClientHello); !ok || value.Credential.NodeID != "node" {
		t.Fatal("wrong Hello subject")
	}
	frame, err := decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := frame.(*relay.ClientFrame); !ok || value.PeerID != "peer" || value.PeerNetworkID != "network" {
		t.Fatal("missing pair binding")
	}
	if writeDeadline.After(transport.credential.ExpiresAt) {
		t.Fatal("I/O exceeded original credential expiry")
	}
}

func TestRelayTransportDoesNotMarshalOrWriteOversizedPayload(t *testing.T) {
	transport, socket, _ := relayTransportFixture(t)
	if err := transport.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	socket.mu.Lock()
	before := socket.output.Len()
	socket.mu.Unlock()
	if err := transport.Send(t.Context(), "peer", make([]byte, relay.MaxFramePayloadBytes+1)); err != ErrPacketIO {
		t.Fatal("oversized encrypted payload accepted", err)
	}
	socket.mu.Lock()
	after := socket.output.Len()
	socket.mu.Unlock()
	if after != before {
		t.Fatal("oversized frame reached socket")
	}
	if !transport.Ready() {
		t.Fatal("bad caller frame unnecessarily disabled working Relay")
	}
}

func TestRelayTransportRejectsUnverifiedTLSAndLateAuthority(t *testing.T) {
	for _, name := range []string{"TLS12", "unverified-chain", "not-handshaken", "offline-before-open", "expired", "revoked-after-dial"} {
		t.Run(name, func(t *testing.T) {
			transport, socket, permission := relayTransportFixture(t)
			switch name {
			case "TLS12":
				socket.state.Version = tls.VersionTLS12
			case "unverified-chain":
				socket.state.VerifiedChains = nil
			case "not-handshaken":
				socket.state.HandshakeComplete = false
			case "offline-before-open":
				permission.set(connectivity.Permission{ExistingFlows: true})
			case "expired":
				transport.credential.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked-after-dial":
				base := transport.dial
				transport.dial = func(c context.Context, a string, v *tls.Config) (relaySocket, error) {
					s, e := base(c, a, v)
					permission.set(connectivity.Permission{Revoked: true})
					return s, e
				}
			}
			if err := transport.Open(t.Context()); err == nil || transport.Ready() {
				t.Fatal("unsafe Relay connection accepted")
			}
		})
	}
}

func TestRelayReceiveNeverAcceptsForeignMetadataOrDataAfterLeaseFailure(t *testing.T) {
	for _, name := range []string{"foreign-node", "foreign-network", "expired-after-read"} {
		t.Run(name, func(t *testing.T) {
			frame := &relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version, FromNodeID: "peer", FromNetworkID: "network", Payload: []byte{1}}
			if name == "foreign-node" {
				frame.FromNodeID = "other"
			}
			if name == "foreign-network" {
				frame.FromNetworkID = "other"
			}
			transport, socket, permission := relayTransportFixture(t, frame)
			if err := transport.Open(t.Context()); err != nil {
				t.Fatal(err)
			}
			if name == "expired-after-read" {
				socket.beforeRead = func() { permission.set(connectivity.Permission{Revoked: true}) }
			}
			if _, payload, err := transport.Receive(t.Context()); err == nil || len(payload) != 0 {
				t.Fatal("foreign/expired payload delivered")
			}
		})
	}
}

func TestRelayWriterRechecksPairAfterWaitingAndRejectsCancelledWait(t *testing.T) {
	transport, socket, _ := relayTransportFixture(t)
	if err := transport.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport.writeGate <- struct{}{}
	result := make(chan error, 1)
	observed := &relayWaitingContext{Context: t.Context(), waiting: make(chan struct{})}
	go func() { result <- transport.Send(observed, "peer", []byte{1}) }()
	<-observed.waiting
	transport.mu.Lock()
	transport.peers = map[string]bool{}
	transport.mu.Unlock()
	<-transport.writeGate
	if err := <-result; err != ErrRelayPeer {
		t.Fatal("removed pair survived writer wait", err)
	}
	socket.mu.Lock()
	raw := append([]byte(nil), socket.output.Bytes()...)
	socket.mu.Unlock()
	decoder := relay.NewDecoder(bytes.NewReader(raw))
	_, _ = decoder.Read()
	if _, err := decoder.Read(); err != io.EOF {
		t.Fatal("unadmitted frame was written")
	}
	transport.writeGate <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := transport.write(ctx, &relay.Heartbeat{Type: relay.MessageHeartbeat, ProtocolVersion: relay.Version, Time: time.Now().UTC().Format(time.RFC3339Nano)}, false); err != context.Canceled {
		t.Fatal("cancelled serialization wait blocked", err)
	}
	<-transport.writeGate
}

type relayWaitingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *relayWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}
