package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestSupervisorRetainsCompletedRelayDialDuringMapHandoff(t *testing.T) {
	connection, f, _, _ := relayPipeFixture(t)
	completed := make(chan supervisorDialResult, 1)
	completed <- supervisorDialResult{transport: connection}
	g := &transportSupervisor{ctx: t.Context(), lease: f.lease, current: f.verified, config: supervisorConfig{maxPeers: 1}, dial: &supervisorDial{cancel: func() {}, done: completed}}
	now, _ := f.clock.Sample()
	f.lease.PausePackets(true)
	g.ensureMux(now)
	if g.dial == nil || g.dial.result == nil || g.mux != nil || !connection.connected() {
		t.Fatal("temporary source fence discarded a completed dial or admitted it as ready")
	}
	g.ensureMux(now)
	if g.dial == nil || !connection.connected() {
		t.Fatal("consumed result was reread or closed on the next actor step")
	}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.refresh(t, &cursor)
	g.current = f.verified
	f.lease.PausePackets(false)
	g.ensureMux(now)
	if g.dial != nil || g.mux == nil || g.mux.transport != connection || !connection.connected() {
		t.Fatal("completed dial was not revalidated against the current map")
	}
	t.Cleanup(g.mux.Close)
}

func TestSupervisorShutdownClosesConsumedPendingDial(t *testing.T) {
	connection, _, _, _ := relayPipeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	g := &transportSupervisor{ctx: ctx, cancel: cancel, done: make(chan struct{}), config: supervisorConfig{probeInterval: time.Second}, dial: &supervisorDial{cancel: func() {}, done: make(chan supervisorDialResult), result: &supervisorDialResult{transport: connection}}}
	go g.run()
	select {
	case <-g.done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited again on an already consumed dial result")
	}
	if connection.connected() {
		t.Fatal("shutdown leaked the pending Relay socket")
	}
}

func TestRelayAdmissionDoesNotStarveBehindUnconfirmedPassivePeers(t *testing.T) {
	f := newCandidateFixture(t)
	base, err := json.Marshal(f.snapshot.Peers[0])
	if err != nil {
		t.Fatal(err)
	}
	f.snapshot.Peers = nil
	for i, id := range []string{"aaa-first", "aaa-second", "peer-third"} {
		var peer coordinator.Peer
		if err := json.Unmarshal(base, &peer); err != nil {
			t.Fatal(err)
		}
		private, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{byte(84 + i)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		peer.ID, peer.PublicKey = id, base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
		peer.AllowedIPs = []string{fmt.Sprintf("100.70.0.%d/32", i+2)}
		peer.ACLGrants[0].DestinationCIDRs = append([]string(nil), peer.AllowedIPs...)
		peer.Transport.PeerNodeID, peer.Transport.PeerPublicKey = peer.ID, peer.PublicKey
		f.snapshot.Peers = append(f.snapshot.Peers, peer)
	}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.refresh(t, &cursor)
	f.withRelay(t)
	connection, _, _, _ := relayPipeFixtureFor(t, f)
	mux, err := newRelayMux(t.Context(), connection, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mux.Close)
	packets, _ := switchFixture(t, f)
	budget, err := newTransportBudget(3)
	if err != nil {
		t.Fatal(err)
	}
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, profile: f.profile.ID, generation: 1, private: f.profile.Keys.WireGuardPrivate, budget: budget, packets: packets, mux: mux, peers: make(map[string]*supervisedRelay), config: supervisorConfig{mtu: 576}}
	t.Cleanup(func() {
		for id := range g.peers {
			g.dropPeer(id)
		}
	})
	g.ensurePeers()
	if len(g.peers) != 2 || g.peers["peer-third"] != nil {
		t.Fatal("single actor step exceeded constructor admission bound")
	}
	g.ensurePeers()
	if g.peers["peer-third"] == nil || g.peers["peer-third"].channel == nil || len(budget.engines) != 3 {
		t.Fatal("passive unconfirmed peers starved the next peer")
	}
	if len(budget.probes) != 0 {
		t.Fatal("construction created own probes or inferred readiness")
	}
	g.retire("aaa-first")
	g.retire("aaa-second")
	g.ensurePeers()
	if g.peers["peer-third"].channel == nil || g.peers["aaa-first"].channel == nil || g.peers["aaa-second"].channel == nil {
		t.Fatal("rotating reconstruction lost unrelated peer channel")
	}
}

func TestSupervisorSelectsActualEncryptedRelayAfterOwnProofAndRoutesNativePackets(t *testing.T) {
	connection, f, sent, inject := relayPipeFixture(t)
	packets, native := switchFixture(t, f)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	slot, err := budget.reserve(f.lease, "", enginePrimary)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := NewEngine(wireGuardPort{packets.primary}, &inertBind{}, 1, 51820, f.lease)
	if err != nil {
		slot.release()
		t.Fatal(err)
	}
	primary.reservation = slot
	t.Cleanup(primary.Close)
	if err := primary.Apply(t.Context(), f.verified, f.profile.Keys.WireGuardPrivate); err != nil {
		t.Fatal(err)
	}
	a := f.assignment(t)
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	seen, gate := make(chan struct{}, 1), make(chan struct{})
	var dropEcho atomic.Bool
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	defer unblock()
	wire := &memoryWireBind{address: a.peer.Endpoint}
	remoteBind := &relayCryptoFixtureBind{cipherFixtureBind: &cipherFixtureBind{memoryWireBind: wire}, emit: func(raw []byte) { inject("peer", raw) }}
	var remote *peerCipher
	applications := make(chan []byte, 1)
	port, err := newPacketPort(t.Context(), 576, f.lease.Permissions, func(raw []byte) error {
		value, claimed, err := decodeControlPacket(raw, local, peer)
		if err != nil {
			return err
		}
		if claimed {
			select {
			case seen <- struct{}{}:
			default:
			}
			if dropEcho.Load() {
				return nil
			}
			select {
			case <-gate:
			case <-t.Context().Done():
				return t.Context().Err()
			}
			reply, err := controlPacket(peer, local, replyControl(value, transport.Echo))
			if err != nil {
				return err
			}
			return remote.Queue(reply, true)
		}
		select {
		case applications <- bytes.Clone(raw):
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, err = newPeerCipher(t.Context(), coordinator.Node{ID: a.peer.ID, PublicKey: a.peer.PublicKey, AssignedIP: peer.String()}, coordinator.Peer{ID: a.local.ID, PublicKey: a.local.PublicKey, AllowedIPs: []string{local.String() + "/32"}, ACLRestricted: true, Endpoint: a.offer.LocalEndpoint}, bytes.Repeat([]byte{84}, 32), relayBaseline(), port, remoteBind)
	if err != nil {
		_ = port.Close()
		t.Fatal(err)
	}
	t.Cleanup(remote.Close)
	go func() {
		for {
			select {
			case <-connection.done:
				return
			case <-t.Context().Done():
				return
			case frame := <-sent:
				socket := wire.socket.Load()
				if socket != nil {
					select {
					case <-socket.done:
						return
					case socket.packets <- memoryWirePacket{data: bytes.Clone(frame.Payload), source: a.offer.LocalEndpoint}:
					}
				}
			}
		}
	}()
	g, err := newTransportSupervisor(t.Context(), f.verified, f.lease, f.clock, f.profile.ID, 1, f.profile.Keys.WireGuardPrivate, budget, packets, primary, supervisorConfig{relay: connection.config, probeInterval: 10 * time.Second, retry: time.Second, mtu: 576, maxPeers: 1, listenCandidate: func(string, string, uint16) (candidateSocket, error) { return newCandidateSocketFixture(), nil }}, func(context.Context, connectivity.Map, string) (*RelayTransport, error) { return connection, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	defer unblock() // release a blocked peer writer before closing its engine
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not send an own challenge through actual WG")
	}
	if len(g.outputChoices()) != 0 {
		t.Fatal("TLS Ready/handshake selected a peer route before own echo")
	}
	for _, printed := range []string{fmt.Sprint(g), fmt.Sprintf("%#v", g)} {
		if printed != "[private peer transport supervisor]" {
			t.Fatal("unsafe supervisor formatting")
		}
	}
	unblock()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for g.outputChoices()["peer"] != peerRouteRelay {
		select {
		case <-deadline.C:
			t.Fatal("own encrypted echo did not select Relay")
		case <-tick.C:
		}
	}
	raw := cipherIPv4(local.String(), peer.String(), 7)
	native.input <- raw
	select {
	case got := <-applications:
		if !bytes.Equal(got, raw) || len(packets.primary.queue) != 0 {
			t.Fatal("native Relay packet changed or was duplicated through primary")
		}
	case <-time.After(time.Second):
		t.Fatal("native packet did not traverse selected encrypted Relay")
	}
	reverse := cipherIPv4(peer.String(), local.String(), 8)
	if err := remote.Queue(reverse, false); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-native.output:
		if !bytes.Equal(got, reverse) {
			t.Fatal("native ingress changed")
		}
	case <-time.After(time.Second):
		t.Fatal("authenticated peer frame did not reach current native sink")
	}
	f.lease.PausePackets(true)
	g.notify()
	if connection.authorityGone() || !connection.connected() || g.outputChoices()["peer"] != peerRouteNone {
		t.Fatal("temporary pause destroyed socket or retained app availability")
	}
	f.lease.PausePackets(false)
	if g.outputChoices()["peer"] != peerRouteRelay {
		t.Fatal("unchanged Relay failed to resume")
	}
	// A known cryptographic session may retain existing flows when the remote
	// peer cannot answer a new health probe (for example its offline gate).
	dropEcho.Store(true)
	f.clock.advance(10*time.Second, 10*time.Second)
	g.notify()
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("next health probe was not scheduled")
	}
	f.clock.advance(4*time.Second, 4*time.Second)
	g.notify()
	expired := time.NewTimer(time.Second)
	defer expired.Stop()
	for {
		budget.mu.Lock()
		pending := len(budget.probes)
		budget.mu.Unlock()
		if pending == 0 {
			break
		}
		select {
		case <-expired.C:
			t.Fatal("expired health permit was retained")
		case <-tick.C:
		}
	}
	if !connection.connected() || g.outputChoices()["peer"] != peerRouteRelay {
		t.Fatal("missed health reply destroyed an established Relay")
	}
	packets.mu.Lock()
	route := packets.routes["peer"]
	packets.mu.Unlock()
	route.mu.Lock()
	channel := route.relay
	route.mu.Unlock()
	channel.sessionLost.Store(true) // synthetic upstream expiry signal, not timer evidence
	if g.outputChoices()["peer"] != peerRouteNone {
		t.Fatal("expired crypto session remained selected")
	}
	channel.sessionLost.Store(false)
	if g.outputChoices()["peer"] != peerRouteNone {
		t.Fatal("new session without fresh own echo resurrected path proof")
	}
	g.Close()
	budget.mu.Lock()
	engines, probes := len(budget.engines), len(budget.probes)
	budget.mu.Unlock()
	if engines != 1 || probes != 0 || !bytes.Equal(g.private, make([]byte, 32)) {
		t.Fatal("completed supervisor close leaked workers/probes/private key")
	}
}

func TestCompiledAdvertisementReturnsIndependentProducerValidatedMetadata(t *testing.T) {
	ad := transportAdvertisement(7)
	if err := coordinator.ValidateTransportAdvertisement(ad); err != nil {
		t.Fatal(err)
	}
	ad.Capabilities.Engines[0].ProtocolVersion = "changed"
	if fresh := transportAdvertisement(7); coordinator.ValidateTransportAdvertisement(fresh) != nil || len(fresh.Endpoints) != 0 {
		t.Fatal("caller changed compiled capabilities or a fictitious listener was advertised")
	}
}
