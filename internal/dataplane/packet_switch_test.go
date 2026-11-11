package dataplane

import (
	"bytes"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wireguard-go/tun"
)

// A plaintext TUN fixture only: these tests prove multiplexing/lifetime gates,
// not Windows WFP classification or authentication of an external peer.
type switchTUN struct {
	*inertTUN
	input, output chan []byte
	reading       chan struct{}
	active, peak  atomic.Int32
	closes        atomic.Int32
}

func newSwitchTUN() *switchTUN {
	return &switchTUN{inertTUN: newInertTUN(), input: make(chan []byte, 4), output: make(chan []byte, 4), reading: make(chan struct{}, 4)}
}
func (t *switchTUN) Read(slab []byte, packets []tun.ReadPacket) (int, error) {
	current := t.active.Add(1)
	defer t.active.Add(-1)
	for previous := t.peak.Load(); current > previous && !t.peak.CompareAndSwap(previous, current); previous = t.peak.Load() {
	}
	t.reading <- struct{}{}
	select {
	case <-t.done:
		return 0, os.ErrClosed
	case raw := <-t.input:
		copy(slab[tun.ReadPacketSpacing:], raw)
		packets[0] = tun.ReadPacket{Offset: tun.ReadPacketSpacing, Size: len(raw)}
		return 1, nil
	}
}
func (t *switchTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, raw := range bufs {
		t.output <- bytes.Clone(raw[offset:])
	}
	return len(bufs), nil
}
func (t *switchTUN) Close() error { t.closes.Add(1); return t.inertTUN.Close() }

func awaitSwitchRead(t *testing.T, native *switchTUN) {
	t.Helper()
	select {
	case <-native.reading:
	case <-time.After(time.Second):
		t.Fatal("native reader did not run")
	}
}

func switchFixture(t *testing.T, f *candidateFixture) (*packetSwitch, *switchTUN) {
	t.Helper()
	native := newSwitchTUN()
	s, err := newPacketSwitch(t.Context(), native, f.lease, f.verified, 1280, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	awaitSwitchRead(t, native)
	return s, native
}

func switchPacket(t *testing.T, s *packetSwitch, reverse bool, payload byte) []byte {
	t.Helper()
	s.mu.Lock()
	b := s.pairs["peer"]
	s.mu.Unlock()
	if reverse {
		return cipherIPv4(b.peer.String(), b.local.String(), payload)
	}
	return cipherIPv4(b.local.String(), b.peer.String(), payload)
}

func nextSwitchPrimary(t *testing.T, s *packetSwitch) portPacket {
	t.Helper()
	type result struct {
		p   portPacket
		err error
	}
	done := make(chan result, 1)
	go func() { p, err := s.primary.next(); done <- result{p, err} }()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.p
	case <-time.After(time.Second):
		t.Fatal("primary queue did not deliver")
		return portPacket{}
	}
}

func TestPacketSwitchOwnsOneNativeReaderAndPrimaryCannotCloseIt(t *testing.T) {
	f := newCandidateFixture(t)
	s, native := switchFixture(t, f)
	raw := switchPacket(t, s, false, 1)
	native.input <- raw
	awaitSwitchRead(t, native) // next Read starts only after processing this packet
	packet := nextSwitchPrimary(t, s)
	if !bytes.Equal(packet.data, raw) || native.peak.Load() != 1 {
		t.Fatal("native packet changed or multiple readers competed")
	}
	if err := s.primary.Close(); err != nil || native.closes.Load() != 0 {
		t.Fatal("virtual primary closed shared native TUN", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil || native.closes.Load() != 1 || native.active.Load() != 0 {
		t.Fatal("shared reader did not finish exactly once", err)
	}
}

func TestPacketSwitchSelectedRouteNeverRetriesPayloadThroughPrimary(t *testing.T) {
	n, f := qualifiedNegotiation(t)
	s, _ := switchFixture(t, f)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	old := switchPacket(t, s, false, 1)
	if err := s.send(old); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("peer", route); err != nil {
		t.Fatal(err)
	}
	// The Relay socket is ready, but this peer route has no own path proof.
	if err := s.send(switchPacket(t, s, false, 2)); !errors.Is(err, ErrNoAuthority) || len(s.primary.queue) != 1 {
		t.Fatal("unconfirmed route replayed payload through primary", err)
	}
	if err := s.SetRoute("peer", nil); err != nil {
		t.Fatal(err)
	}
	current := switchPacket(t, s, false, 3)
	if err := s.send(current); err != nil {
		t.Fatal(err)
	}
	packet := nextSwitchPrimary(t, s)
	if !bytes.Equal(packet.data, current) || len(s.primary.queue) != 0 {
		t.Fatal("old primary output epoch survived route replacement")
	}
	if err := route.ConfirmRelay(routeRelayProof(t, n, f)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("peer", route); err != nil {
		t.Fatal(err)
	}
	if err := s.send(current); err != nil || len(s.primary.queue) != 0 {
		t.Fatal("confirmed Relay payload fanned out to primary", err)
	}
}

func TestPacketSwitchIngressRequiresCurrentPairAndConsumesControl(t *testing.T) {
	f := newCandidateFixture(t)
	s, native := switchFixture(t, f)
	sink, err := s.Sink(f.verified, "peer")
	if err != nil {
		t.Fatal(err)
	}
	raw := switchPacket(t, s, true, 1)
	if err := sink(raw); err != nil {
		t.Fatal(err)
	}
	if got := <-native.output; !bytes.Equal(got, raw) {
		t.Fatal("ingress plaintext changed")
	}
	wrong := bytes.Clone(raw)
	wrong[12]++
	if err := sink(wrong); err == nil {
		t.Fatal("foreign plaintext source reached native TUN")
	}
	s.mu.Lock()
	b := s.pairs["peer"]
	s.mu.Unlock()
	control := transport.Control{Version: 1, Kind: transport.Challenge, AccountID: b.account, NetworkID: b.network, SourceNodeID: b.peerID, DestinationNodeID: b.localID, SourcePublicKey: b.peerKey, DestinationPublicKey: b.localKey, SourceGeneration: b.peerGeneration, DestinationGeneration: b.localGeneration, NetworkRevision: 1, ProfileDigest: f.snapshot.Peers[0].Transport.Profiles[0].Digest, CandidateID: "00112233445566778899aabbccddeeff", Nonce: bytes.Repeat([]byte{4}, 32)}
	reserved, err := controlPacket(b.peer, b.local, control)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink(reserved); err == nil {
		t.Fatal("reserved control delivered to native OS")
	}
	if err := s.primaryIngress(reserved); err != nil || len(native.output) != 0 {
		t.Fatal("primary control escaped common native boundary", err)
	}
	previous := f.verified.Cursor()
	f.clock.advance(time.Second, time.Second)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision++
	f.snapshot.Peers[0].Transport.PeerAdvertisement.IntentGeneration++
	f.refresh(t, &previous)
	if err := sink(raw); err == nil {
		t.Fatal("old map delivered between Confirm and native scope update")
	}
	if err := s.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if err := sink(raw); err == nil || len(native.output) != 0 {
		t.Fatal("old peer generation retained its native sink", err)
	}
	current, err := s.Sink(f.verified, "peer")
	if err != nil || current(raw) != nil || len(native.output) != 1 {
		t.Fatal("new current sink not admitted", err)
	}
	f.lease.Revoke()
	if err := current(raw); err == nil || len(native.output) != 1 {
		t.Fatal("revoked sink delivered plaintext", err)
	}
}

func TestPacketSwitchRejectsUnsealedOrForeignRouteAndReportsReaderFailure(t *testing.T) {
	f := newCandidateFixture(t)
	native := newSwitchTUN()
	if _, err := newPacketSwitch(t.Context(), native, f.lease, connectivity.Map{}, 1280, 8); err == nil || native.closes.Load() != 0 {
		t.Fatal("unsealed map accepted or caller native ownership stolen", err)
	}
	s, _ := switchFixture(t, f)
	other, otherFixture := qualifiedNegotiation(t)
	route, err := newPeerRoute(other.relay, otherFixture.clock, otherFixture.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("peer", route); err == nil {
		t.Fatal("different lease's route installed")
	}
	_ = s.native.Close() // unexpected native failure, not owner shutdown
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("native reader did not terminate")
	}
	if !errors.Is(s.Err(), ErrPacketIO) {
		t.Fatal("native reader failure was hidden", s.Err())
	}
}
