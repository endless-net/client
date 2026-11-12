package dataplane

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	relay "github.com/endless-net/relay/api/wire"
	signing "github.com/endless-net/signing/api/contract"
)

func (f *candidateFixture) withRelay(t *testing.T) {
	t.Helper()
	sample, _ := f.clock.Sample()
	expires := sample.Wall.Add(30 * time.Second)
	var trust signing.ServerKeyResponse
	if err := json.Unmarshal(f.trust.Bundle, &trust); err != nil {
		t.Fatal(err)
	}
	credential := &relay.Credential{Algorithm: relay.CredentialAlgorithm, KeyID: trust.RelayTrustBundle.ActiveKeyID, NetworkID: f.snapshot.Network.ID, NodeID: f.snapshot.Node.ID, ExpiresAt: expires}
	payload, err := relay.CredentialSigningPayload(credential.KeyID, credential.NetworkID, credential.NodeID, expires)
	if err != nil {
		t.Fatal(err)
	}
	credential.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{83}, 32)), payload))
	f.snapshot.RelayCredential = credential
	f.snapshot.Relays = []relay.Endpoint{{ID: "edge", Addr: "relay.example:443", Protocol: relay.EndpointProtocolTLS}}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
}

// net.Pipe and the injected TLS state exercise published frame I/O only. This
// fixture does not claim a real TLS handshake or external Relay authentication.
type relayPipeSocket struct{ net.Conn }

func (*relayPipeSocket) ConnectionState() tls.ConnectionState {
	return tls.ConnectionState{Version: tls.VersionTLS13, HandshakeComplete: true, VerifiedChains: [][]*x509.Certificate{{{}}}}
}

func relayPipeFixture(t *testing.T) (*RelayTransport, *candidateFixture, <-chan *relay.ClientFrame, func(string, []byte)) {
	t.Helper()
	f := newCandidateFixture(t)
	f.withRelay(t)
	return relayPipeFixtureFor(t, f)
}

func relayPipeFixtureFor(t *testing.T, f *candidateFixture) (*RelayTransport, *candidateFixture, <-chan *relay.ClientFrame, func(string, []byte)) {
	t.Helper()
	transport, err := NewRelayTransport(t.Context(), f.verified, f.lease, "edge", RelayConfig{Now: func() time.Time { s, _ := f.clock.Sample(); return s.Wall }, DialTimeout: time.Second, IOTimeout: 2 * time.Second, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	first, second := net.Pipe()
	t.Cleanup(func() { _ = second.Close() })
	transport.dial = func(context.Context, string, *tls.Config) (relaySocket, error) { return &relayPipeSocket{first}, nil }
	sent := make(chan *relay.ClientFrame, 16)
	var writer sync.Mutex
	write := func(message relay.Message) error {
		raw, err := relay.MarshalMessage(message)
		if err != nil {
			return err
		}
		writer.Lock()
		defer writer.Unlock()
		_ = second.SetWriteDeadline(time.Now().Add(time.Second))
		_, err = second.Write(raw)
		return err
	}
	go func() {
		decoder := relay.NewDecoder(second)
		message, err := decoder.Read()
		if err != nil {
			return
		}
		if _, ok := message.(*relay.ClientHello); !ok {
			return
		}
		if write(&relay.Ready{Type: relay.MessageReady, ProtocolVersion: relay.Version, Ready: true}) != nil {
			return
		}
		for {
			message, err := decoder.Read()
			if err != nil {
				return
			}
			if heartbeat, ok := message.(*relay.Heartbeat); ok {
				if write(&relay.Heartbeat{Type: relay.MessageHeartbeat, ProtocolVersion: relay.Version, Time: heartbeat.Time}) != nil {
					return
				}
				continue
			}
			if frame, ok := message.(*relay.ClientFrame); ok {
				select {
				case sent <- frame:
				case <-t.Context().Done():
					return
				}
			}
		}
	}()
	if err := transport.Open(t.Context()); err != nil {
		transport.Close()
		t.Fatal(err)
	}
	t.Cleanup(transport.Close)
	inject := func(peer string, raw []byte) {
		t.Helper()
		if err := write(&relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version, FromNodeID: peer, FromNetworkID: "network", Payload: raw}); err != nil {
			// The remote cipher can finish a queued frame after the supervisor
			// deliberately closes its transport. That is normal fixture teardown.
			if errors.Is(err, io.ErrClosedPipe) {
				select {
				case <-transport.done:
					return
				default:
				}
			}
			t.Fatal(err)
		}
	}
	return transport, f, sent, inject
}

func relayMuxFixture(t *testing.T) (*relayMux, *candidateFixture, <-chan *relay.ClientFrame, func(string, []byte)) {
	t.Helper()
	transport, f, sent, inject := relayPipeFixture(t)
	mux, err := newRelayMux(t.Context(), transport, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mux.Close)
	return mux, f, sent, inject
}

func TestRelayLogicalReceiverCanFinishConstructionWhileSourcePaused(t *testing.T) {
	mux, f, sent, _ := relayMuxFixture(t)
	bind, err := mux.newBind(t.Context(), f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bind.shutdown)
	port, err := newPacketPort(t.Context(), 1280, bind.permissions, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	local, peer := bind.local, bind.peer
	peer.Endpoint = bind.endpoint.String()
	f.lease.PausePackets(true)
	cipher, err := buildPeerCipher(t.Context(), local, peer, f.profile.Keys.WireGuardPrivate, relayBaseline(), port, bind, nil, nil)
	if err != nil {
		t.Fatal("source handoff destroyed a constructed logical receiver", err)
	}
	t.Cleanup(cipher.Close)
	if bind.permissions().ExistingFlows || cipher.bind.port.Load() != 1 {
		t.Fatal("logical Open bypassed the traffic fence or failed to finish")
	}
	endpoint, err := bind.ParseEndpoint(peer.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := bind.Send([][]byte{{1}}, endpoint); err == nil {
		t.Fatal("source-paused channel transmitted a packet")
	}
	bind.enqueue([]byte{1})
	if len(bind.queue.frames) != 0 {
		t.Fatal("source-paused channel admitted received ciphertext")
	}
	select {
	case <-sent:
		t.Fatal("logical receiver construction transmitted a frame")
	default:
	}
	f.lease.PausePackets(false)
	if !bind.permissions().NewFlows {
		t.Fatal("unchanged current scope did not resume")
	}
}

func TestRelayMuxUsesProducerFramesAndBoundedExactPeerBind(t *testing.T) {
	mux, f, sent, inject := relayMuxFixture(t)
	bind, err := mux.newBind(t.Context(), f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mux.newBind(t.Context(), f.verified, f.lease, "peer"); err == nil {
		t.Fatal("duplicate peer allocated another engine bind")
	}
	if _, err := mux.newBind(t.Context(), f.verified, f.lease, "foreign"); err == nil {
		t.Fatal("foreign peer assigned")
	}
	functions, port, err := bind.Open(0)
	if err != nil || port != 1 || len(functions) != 1 {
		t.Fatal("logical engine bind failed", port, err)
	}
	endpoint, err := bind.ParseEndpoint("192.0.2.20:51820")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bind.ParseEndpoint("host.example:443"); err == nil {
		t.Fatal("arbitrary Relay endpoint accepted")
	}
	if err := bind.SetMark(1); err == nil {
		t.Fatal("unexpected mark silently accepted")
	}
	if err := bind.SetMark(0); err != nil || bind.BatchSize() != 1 {
		t.Fatal(err)
	}
	if err := bind.Send([][]byte{{1, 2, 3}}, endpoint); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-sent:
		if frame.PeerID != "peer" || frame.PeerNetworkID != "network" || !bytes.Equal(frame.Payload, []byte{1, 2, 3}) {
			t.Fatal("producer frame scope lost")
		}
	case <-time.After(time.Second):
		t.Fatal("encrypted frame not sent")
	}
	inject("peer", []byte{4, 5, 6})
	raw := make([]byte, 64)
	sizes := []int{0}
	endpoints := make([]awgconn.Endpoint, 1)
	if n, err := functions[0]([][]byte{raw}, sizes, endpoints); err != nil || n != 1 || sizes[0] != 3 || !bytes.Equal(raw[:3], []byte{4, 5, 6}) || endpoints[0].DstToString() != endpoint.DstToString() {
		t.Fatal("peer demultiplexing failed", n, err)
	}
	bind.shutdown()
	if _, err := functions[0]([][]byte{raw}, sizes, endpoints); !errors.Is(err, net.ErrClosed) {
		t.Fatal("shutdown left receiver alive", err)
	}
	mux.mu.Lock()
	remaining := len(mux.peers)
	mux.mu.Unlock()
	if remaining != 0 {
		t.Fatal("shutdown retained peer slot")
	}
}

func TestRelayMuxQueueLimitsAndCloseOpenLifecycle(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	bind, err := mux.newBind(t.Context(), f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bind.Open(0); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		bind.enqueue(make([]byte, 32768))
	}
	bind.mu.Lock()
	queue := bind.queue
	bind.mu.Unlock()
	queue.mu.Lock()
	count, used := len(queue.frames), queue.bytes
	queue.mu.Unlock()
	if count != 2 || used != 65536 {
		t.Fatal("peer queue exceeded byte bound", count, used)
	}
	if err := bind.Close(); err != nil {
		t.Fatal(err)
	}
	queue.mu.Lock()
	used = queue.bytes
	queue.mu.Unlock()
	if used != 0 {
		t.Fatal("close retained ciphertext")
	}
	if _, _, err := bind.Open(0); err != nil {
		t.Fatal("upstream Close/Open rejected", err)
	}
	for range 17 {
		bind.enqueue([]byte{1})
	}
	bind.mu.Lock()
	queue = bind.queue
	bind.mu.Unlock()
	queue.mu.Lock()
	count = len(queue.frames)
	queue.mu.Unlock()
	if count != 16 {
		t.Fatal("peer frame bound lost", count)
	}
	mux.Close()
	select {
	case <-mux.done:
	case <-time.After(time.Second):
		t.Fatal("mux reader did not stop")
	}
}

func TestRelayMuxMapRenewalFencesStalePairWithoutDiscardingTLS(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	bind, err := mux.newBind(t.Context(), f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bind.Open(0); err != nil {
		t.Fatal(err)
	}
	endpoint, err := bind.ParseEndpoint("192.0.2.20:51820")
	if err != nil {
		t.Fatal(err)
	}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if err := bind.Send([][]byte{{1}}, endpoint); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("stale pair cache admitted frame", err)
	}
	if mux.transport.Ready() {
		t.Fatal("stale transport binding reported current")
	}
	if err := mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if !mux.transport.Ready() {
		t.Fatal("renewal discarded valid original TLS socket")
	}
	if err := bind.Send([][]byte{{2}}, endpoint); err != nil {
		t.Fatal("renewed pair failed", err)
	}
	cursor = f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Peers[0].ACLGrants = nil
	f.snapshot.Peers[0].IngressACLGrants = nil
	f.refresh(t, &cursor)
	if err := mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if bind.permissions().ExistingFlows {
		t.Fatal("zero-grant peer retained Relay bind")
	}
	if err := bind.Send([][]byte{{3}}, endpoint); err == nil {
		t.Fatal("removed peer emitted frame")
	}
}

func TestRelayPeerIntentChangeRetiresOldCipherBinding(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	channel, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.PeerAdvertisement.IntentGeneration++
	f.refresh(t, &cursor)
	if err := mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	select {
	case <-channel.done:
	case <-time.After(time.Second):
		t.Fatal("old peer generation retained crypto worker/session")
	}
	if channel.bind.permissions().ExistingFlows {
		t.Fatal("old Relay health/session survived peer intent change")
	}
	if !mux.transport.Ready() {
		t.Fatal("one peer restart destroyed shared TLS connection")
	}
}
