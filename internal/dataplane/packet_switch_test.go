package dataplane

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
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
	select {
	case t.reading <- struct{}{}:
	default:
	}
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

func TestPacketGrantDeadlineCombinesRulesByDirection(t *testing.T) {
	first, last := time.Unix(1800000000, 0), time.Unix(1800000060, 0)
	rule := func(port int, end *time.Time) coordinator.ACLGrant {
		return coordinator.ACLGrant{AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: port}}, ValidUntil: end}
	}
	packet := cipherIPv4("100.70.0.1", "100.70.0.2", 1)
	peer := coordinator.Peer{ACLGrants: []coordinator.ACLGrant{rule(443, &first), rule(443, &last)}}
	if end, _ := windowsForPeer(peer).peerPacketWindow(packet, true); !end.Equal(last) {
		t.Fatal("a duplicate expired before its independent authorization", end)
	}
	peer.IngressACLGrants = []coordinator.ACLGrant{rule(12000, &first)}
	if end, _ := windowsForPeer(peer).peerPacketWindow(packet, true); !end.Equal(first) {
		t.Fatal("possible reply escaped the inbound rule deadline", end)
	}
	peer.ACLGrants = append(peer.ACLGrants, coordinator.ACLGrant{AllowedPorts: []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}})
	peer.IngressACLGrants = nil
	if end, _ := windowsForPeer(peer).peerPacketWindow(packet, true); !end.IsZero() {
		t.Fatal("an independent all-protocol rule lost access", end)
	}
}

func TestTCPFragmentsUseOnlyApplicableGrantDeadlines(t *testing.T) {
	udpEnd, tcpEnd := time.Unix(1800000000, 0), time.Unix(1800000060, 0)
	peer := coordinator.Peer{ACLGrants: []coordinator.ACLGrant{
		{AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 53}}, ValidUntil: &udpEnd},
		{AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}},
	}}
	// IPv4 options move the TCP header to byte 24. The first fragment
	// includes the TCP header and four payload bytes.
	first := make([]byte, 48)
	first[0], first[9], first[36] = 0x46, 6, 0x50
	binary.BigEndian.PutUint16(first[2:4], uint16(len(first)))
	binary.BigEndian.PutUint16(first[6:8], 0x2000)
	binary.BigEndian.PutUint16(first[24:26], 12000)
	binary.BigEndian.PutUint16(first[26:28], 22)
	later := bytes.Clone(first)
	binary.BigEndian.PutUint16(later[6:8], 3)
	g := windowsForPeer(peer)
	if _, ok := g.peerPacketWindow(later, true); ok {
		t.Fatal("later fragment supplied an unverified transport port")
	}
	peer.ACLGrants = append(peer.ACLGrants, coordinator.ACLGrant{
		AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}, ValidUntil: &tcpEnd,
	})
	g = windowsForPeer(peer)
	var fragments tcpFragments
	key := tcpFragmentKey{peer: "router", outbound: true}
	firstEnd, firstOK := fragments.window(key, 1, g, first, udpEnd)
	laterEnd, laterOK := fragments.window(key, 1, g, later, tcpEnd.Add(-31*time.Second))
	if !firstOK || !laterOK || !firstEnd.IsZero() || !laterEnd.IsZero() {
		t.Fatal("first fragment lost its transport tuple or later payload supplied a false port")
	}
	binary.BigEndian.PutUint16(first[26:28], 443)
	if end, ok := g.peerPacketWindow(first, true); !ok || !end.Equal(tcpEnd) {
		t.Fatal("first fragment lost the port deadline")
	}
	other := key
	other.id++
	if end, ok := fragments.window(other, 1, g, first, udpEnd); !ok || !end.Equal(tcpEnd) {
		t.Fatal("missing expiring HTTPS grant")
	}
	if end, ok := fragments.window(other, 1, g, later, udpEnd); !ok || !end.Equal(tcpEnd) {
		t.Fatal("later HTTPS fragment lost its deadline")
	}
	if _, ok := fragments.window(other, 2, g, later, udpEnd); ok {
		t.Fatal("fragment survived a grant change")
	}
	if _, ok := fragments.window(other, 1, g, later, udpEnd.Add(30*time.Second)); ok {
		t.Fatal("fragment survived the reassembly interval")
	}
	binary.BigEndian.PutUint16(later[6:8], 1)
	if _, ok := fragments.window(key, 1, g, later, udpEnd); ok {
		t.Fatal("fragment overlaps TCP header")
	}
	binary.BigEndian.PutUint16(later[6:8], 3)
	if _, ok := fragments.window(key, 1, g, first, udpEnd); ok {
		t.Fatal("ambiguous IP ID accepted")
	}
	if _, ok := fragments.window(key, 1, g, later, udpEnd); ok {
		t.Fatal("ambiguous continuation accepted")
	}
	first[36] = 0xf0
	binary.BigEndian.PutUint16(first[26:28], 22)
	if _, ok := g.peerPacketWindow(first, true); ok {
		t.Fatal("incomplete TCP options trusted as a full header")
	}
	if _, _, ok := outboundFlowPort(first); ok {
		t.Fatal("fragment supplied a port for flow accounting")
	}
}

func TestPacketSwitchExpiresQueuedPortWhileOffline(t *testing.T) {
	f := newCandidateFixture(t)
	prev := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	sample, _ := f.clock.Sample()
	end := sample.Wall.Add(time.Second)
	p := &f.snapshot.Peers[0]
	p.ACLGrants = []coordinator.ACLGrant{
		{DestinationCIDRs: p.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 443}}, ValidUntil: &end},
		{DestinationCIDRs: p.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 53}}},
	}
	if err := f.verifyResult(t, &prev); !errors.Is(err, connectivity.ErrMap) {
		t.Fatal("unconfigured backend accepted a grant shorter than the map", err)
	}
	f.independentGrantExpiry = true
	f.refresh(t, &prev)
	s, _ := switchFixture(t, f)
	expiring := switchPacket(t, s, false, 1)
	stable := switchPacket(t, s, false, 2)
	binary.BigEndian.PutUint16(stable[22:24], 53)
	for _, packet := range [][]byte{expiring, stable} {
		if err := s.send(packet); err != nil {
			t.Fatal(err)
		}
	}
	f.lease.Unavailable()
	f.clock.advance(0, time.Second)
	if got := nextSwitchPrimary(t, s); !bytes.Equal(got.data, stable) {
		t.Fatal("offline dequeue delivered the expired packet")
	}
	if err := s.send(expiring); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("expired grant accepted another packet", err)
	}
	sink, err := s.Sink(f.verified, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	reply := switchPacket(t, s, true, 3)
	binary.BigEndian.PutUint16(reply[20:22], 443)
	binary.BigEndian.PutUint16(reply[22:24], 12000)
	if err := sink(reply); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("expired return packet reached native session", err)
	}
}

func TestPacketSwitchRechecksQueuedAuthorityAtSameRevision(t *testing.T) {
	f := newCandidateFixture(t)
	s, _ := switchFixture(t, f)
	update := func() {
		t.Helper()
		previous := f.verified.Cursor()
		f.clock.advance(time.Millisecond, time.Millisecond)
		f.refresh(t, &previous)
		if err := s.Update(f.verified); err != nil {
			t.Fatal(err)
		}
	}
	enqueue := func(marker byte) []byte {
		t.Helper()
		raw := switchPacket(t, s, false, marker)
		if err := s.send(raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	first := enqueue(1)
	update()
	if packet := nextSwitchPrimary(t, s); !bytes.Equal(packet.data, first) {
		t.Fatal("unchanged grants lost queued output on signature renewal")
	}
	enqueue(2)
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	update()
	current := enqueue(3)
	if packet := nextSwitchPrimary(t, s); !bytes.Equal(packet.data, current) {
		t.Fatal("queued output survived a changed grant at the same revision")
	}
	enqueue(4)
	saved := f.snapshot.Peers
	f.snapshot.Peers = nil
	update()
	f.snapshot.Peers = saved
	update()
	restored := enqueue(5)
	if packet := nextSwitchPrimary(t, s); !bytes.Equal(packet.data, restored) {
		t.Fatal("restoring the same peer revived output queued before removal")
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

func TestPeerRouteChangePreservesOtherPeerQueue(t *testing.T) {
	n, f := qualifiedNegotiation(t)
	s, native := switchFixture(t, f)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("peer", route); err != nil {
		t.Fatal(err)
	}
	other := f.snapshot.Peers[0]
	otherKey, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{35}, 32))
	if err != nil {
		t.Fatal(err)
	}
	other.ID, other.PublicKey = "second", base64.StdEncoding.EncodeToString(otherKey.PublicKey().Bytes())
	other.Endpoint = "192.0.2.30:51820"
	other.AllowedIPs = []string{"100.70.0.3/32"}
	other.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: other.AllowedIPs, AllowedPorts: other.ACLGrants[0].AllowedPorts}}
	other.Transport = nil
	f.snapshot.Peers = append(f.snapshot.Peers, other)
	update := func() {
		t.Helper()
		previous := f.verified.Cursor()
		f.clock.advance(time.Millisecond, time.Millisecond)
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision++
		f.refresh(t, &previous)
		if err := s.Update(f.verified); err != nil {
			t.Fatal(err)
		}
	}
	update()
	local := f.snapshot.Node.AssignedIP
	out := cipherIPv4(local, "100.70.0.3", 17)
	if err := s.send(out); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute("peer", nil); err != nil {
		t.Fatal(err)
	}
	if got := nextSwitchPrimary(t, s); !bytes.Equal(got.data, out) {
		t.Fatal("changing the first peer's route lost the second peer's queued packet")
	}
	revoked, err := s.Sink(f.verified, "peer")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := s.Sink(f.verified, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := cipherIPv4(local, "100.70.0.2", 18)
	if err := s.send(first); err != nil {
		t.Fatal(err)
	}
	f.snapshot.Peers = f.snapshot.Peers[1:]
	update()
	if err := s.send(first); !errors.Is(err, ErrNoAuthority) {
		t.Fatalf("removed peer's outbound packet: %v", err)
	}
	if err := revoked(cipherIPv4("100.70.0.2", local, 19)); !errors.Is(err, ErrNoAuthority) {
		t.Fatalf("removed peer's ingress: %v", err)
	}
	in := cipherIPv4("100.70.0.3", local, 20)
	if err := retained(in); err != nil || !bytes.Equal(<-native.output, in) {
		t.Fatal("unchanged peer lost its ingress", err)
	}
	if err := s.send(out); err != nil {
		t.Fatal(err)
	}
	if got := nextSwitchPrimary(t, s); !bytes.Equal(got.data, out) || len(s.primary.queue) != 0 {
		t.Fatal("removed peer's queued packet survived its signed revocation")
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

func TestOutboundFlowPortRejectsFragmentsAndIncompleteHeaders(t *testing.T) {
	tcp := make([]byte, 40)
	tcp[0], tcp[9], tcp[22], tcp[23], tcp[32] = 0x45, 6, 1, 187, 0x50
	udp := make([]byte, 28)
	udp[0], udp[9], udp[23], udp[25] = 0x45, 17, 53, 8
	for _, tc := range []struct {
		p        []byte
		protocol string
		port     uint16
	}{{tcp, "tcp", 443}, {udp, "udp", 53}} {
		protocol, port, ok := outboundFlowPort(tc.p)
		if !ok || protocol != tc.protocol || port != tc.port {
			t.Fatal("incorrect outbound tuple")
		}
		frag := bytes.Clone(tc.p)
		frag[6] = 0x20
		for _, bad := range [][]byte{frag, tc.p[:len(tc.p)-1]} {
			if _, _, ok := outboundFlowPort(bad); ok {
				t.Fatal("malformed or fragmented header produced a port")
			}
		}
	}
}

func TestOfflineEchoUsesDirectionalGrantInQueue(t *testing.T) {
	f := newCandidateFixture(t)
	prev := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	sample, _ := f.clock.Sample()
	end := sample.Wall.Add(time.Second)
	p := &f.snapshot.Peers[0]
	all := []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}
	p.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: p.AllowedIPs, AllowedPorts: all}}
	p.IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{f.snapshot.Node.AssignedIP + "/32"}, AllowedPorts: all, ValidUntil: &end}}
	f.independentGrantExpiry = true
	f.refresh(t, &prev)
	s, _ := switchFixture(t, f)
	echo := func(inbound bool, kind byte) []byte {
		packet := switchPacket(t, s, inbound, 1)
		packet[9], packet[20], packet[21] = 1, kind, 0
		packet[10], packet[11], packet[22], packet[23] = 0, 0, 0, 0
		binary.BigEndian.PutUint16(packet[10:12], packetChecksum(packet[:20], 0))
		binary.BigEndian.PutUint16(packet[22:24], packetChecksum(packet[20:], 0))
		return packet
	}
	reply, request := echo(false, 0), echo(false, 8)
	for _, packet := range [][]byte{reply, request} {
		if err := s.send(packet); err != nil {
			t.Fatal(err)
		}
	}
	f.lease.Unavailable()
	f.clock.advance(0, time.Second)
	if got := nextSwitchPrimary(t, s); !bytes.Equal(got.data, request) {
		t.Fatal("expired reply left the primary queue")
	}
	sink, err := s.Sink(f.verified, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink(echo(true, 8)); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("expired incoming echo request accepted", err)
	}
	if err := sink(echo(true, 0)); err != nil {
		t.Fatal("unexpired reply rejected", err)
	}
	if err := s.send(echo(false, 3)); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("related ICMP error used an echo grant", err)
	}
}

func TestRecipientResourcePacketsBindServicePortRouterAndExpiry(t *testing.T) {
	f := newCandidateFixture(t)
	s, native := switchFixture(t, f)
	sample, _ := f.clock.Sample()
	s.mu.Lock()
	b := s.pairs["peer"]
	delete(s.byIP, b.peer)
	s.resources = resourcePacketRules{
		netip.MustParsePrefix("10.72.0.0/24"): {peer: b.peerID, end: sample.Wall.Add(time.Second), ports: map[coordinator.ACLPort]bool{{Protocol: "udp", Port: 443}: true}},
		netip.MustParsePrefix("10.72.0.9/32"): {},
	}
	s.mu.Unlock()
	out := cipherIPv4(b.local.String(), "10.72.0.8", 1)
	if err := s.send(out); err != nil {
		t.Fatal(err)
	}
	if got := nextSwitchPrimary(t, s); !bytes.Equal(got.data, out) {
		t.Fatal("resource packet lost")
	}
	for _, raw := range [][]byte{cipherIPv4(b.local.String(), b.peer.String(), 1), cipherIPv4(b.local.String(), "10.72.0.9", 1), cipherIPv4("10.1.1.1", "10.72.0.8", 1)} {
		if err := s.send(raw); !errors.Is(err, ErrNoAuthority) {
			t.Fatal("accepted unrelated tuple", err)
		}
	}
	reply := cipherIPv4("10.72.0.8", b.local.String(), 1)
	if err := s.ingress(b, reply); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("unrelated source port", err)
	}
	binary.BigEndian.PutUint16(reply[20:22], 443)
	binary.BigEndian.PutUint16(reply[22:24], 12000)
	if err := s.primaryIngress(reply); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-native.output:
		if !bytes.Equal(got, reply) {
			t.Fatal("wrong native payload")
		}
	default:
		t.Fatal("missing reply")
	}
	if err := s.send(out); err != nil {
		t.Fatal(err)
	}
	f.lease.Unavailable()
	f.clock.advance(0, time.Second)
	packet := <-s.primary.queue
	if packet.guard() {
		t.Fatal("expired assignment retained queued packet")
	}
	if err := s.ingress(b, reply); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("expired reply", err)
	}
}

func TestRouterPacketsReachTUNAndReturnToAuthorizedSource(t *testing.T) {
	f := newCandidateFixture(t)
	sw, native := switchFixture(t, f)
	m := f.snapshot
	src := m.Peers[0]
	host := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	subject := coordinator.ServiceHost{NodeID: src.ID, PublicKey: src.PublicKey}
	end := m.MapSignature.ExpiresAt
	m.Node.EndpointExpiresAt = &end
	m.Peers[0].EndpointExpiresAt = &end
	target := "10.17.5.0/24"
	app := coordinator.Application{ID: "lan", TargetType: "cidr", Target: target, AllowedPorts: []coordinator.ServicePort{{Protocol: "udp", Port: 443}}, Connectors: []coordinator.ServiceHost{host}, Sources: []coordinator.ServiceHost{subject}, Routes: []coordinator.ApplicationRoute{{Connector: host, ExpiresAt: end, CIDRs: []string{target}, Metric: 1}}}
	m.Network.Applications = []coordinator.Application{app, {ID: "offline", TargetType: "ip", Target: "10.17.5.9", Connectors: []coordinator.ServiceHost{host}}}
	transit, subjects, err := routerPacketPolicy(m)
	if err != nil || !subjects[src.ID] {
		t.Fatal(err, subjects)
	}
	sw.mu.Lock()
	b := sw.pairs[src.ID]
	delete(sw.byIP, b.peer)
	sw.transit = transit
	sw.mu.Unlock()
	request := cipherIPv4(b.peer.String(), "10.17.5.8", 1)
	if err := sw.primaryIngress(request); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-native.output:
		if !bytes.Equal(raw, request) {
			t.Fatal("changed request")
		}
	default:
		t.Fatal("missing forwarded request")
	}
	reply := cipherIPv4("10.17.5.8", b.peer.String(), 2)
	binary.BigEndian.PutUint16(reply[20:22], 443)
	binary.BigEndian.PutUint16(reply[22:24], 12000)
	if err := sw.send(reply); err != nil {
		t.Fatal(err)
	}
	queued := nextSwitchPrimary(t, sw)
	if !bytes.Equal(queued.data, reply) || !queued.guard() {
		t.Fatal("missing authorized reply")
	}
	a := candidateAssignment{local: m.Node, peer: src}
	for _, tc := range []struct {
		raw []byte
		out bool
	}{{request, false}, {reply, true}} {
		deadline, err := peerPacketDeadline(a, nil, transit, tc.raw, tc.out)
		if err != nil || !deadline.Equal(end) {
			t.Fatal("relay leg", tc.out, err, deadline)
		}
	}
	for _, dst := range []string{"10.17.5.9", "10.17.6.1", b.local.String()} {
		raw := cipherIPv4(b.peer.String(), dst, 1)
		if err := sw.ingress(b, raw); !errors.Is(err, ErrNoAuthority) {
			t.Fatal("out-of-scope forwarding", dst, err)
		}
	}
	binary.BigEndian.PutUint16(reply[20:22], 8443)
	if err := sw.send(reply); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("wrong return port", err)
	}
	if err := sw.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if queued.guard() {
		t.Fatal("retained resource reply after withdrawal")
	}
	if err := sw.ingress(b, request); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("forwarded after withdrawal", err)
	}
}

// Model the packets of two established connections on the shared peer.
// The TUN and clock are in memory, including while the map source is offline.
func TestExistingTCPPortWithdrawnWhileIndependentPortContinues(t *testing.T) {
	for _, event := range []string{"expiry", "withdrawal"} {
		t.Run(event, func(t *testing.T) {
			f := newCandidateFixture(t)
			previous := f.verified.Cursor()
			f.clock.advance(time.Millisecond, time.Millisecond)
			sample, _ := f.clock.Sample()
			end := sample.Wall.Add(time.Second)
			peer := &f.snapshot.Peers[0]
			peer.ACLGrants = []coordinator.ACLGrant{
				{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}, ValidUntil: &end},
				{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}},
			}
			peer.IngressACLGrants = nil
			f.independentGrantExpiry = true
			f.refresh(t, &previous)
			s, native := switchFixture(t, f)
			sink, err := s.Sink(f.verified, peer.ID)
			if err != nil {
				t.Fatal(err)
			}
			packet := func(reply bool, port uint16, flags byte) []byte {
				p := make([]byte, 41)
				copy(p, switchPacket(t, s, reply, 0)[:20])
				p[9], p[32], p[33], p[40] = 6, 0x50, flags, 1
				binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
				src, dst := uint16(12000), port
				if reply {
					src, dst = dst, src
				}
				binary.BigEndian.PutUint16(p[20:22], src)
				binary.BigEndian.PutUint16(p[22:24], dst)
				return p
			}
			outgoing := func(port uint16, flags byte) {
				t.Helper()
				p := packet(false, port, flags)
				if err := s.send(p); err != nil {
					t.Fatal(port, err)
				}
				if !bytes.Equal(nextSwitchPrimary(t, s).data, p) {
					t.Fatal("unexpected queued segment")
				}
			}
			incoming := func(port uint16, flags byte) {
				t.Helper()
				p := packet(true, port, flags)
				if err := sink(p); err != nil {
					t.Fatal(port, err)
				}
				select {
				case delivered := <-native.output:
					if !bytes.Equal(delivered, p) {
						t.Fatal("unexpected incoming segment")
					}
				default:
					t.Fatal("missing incoming segment")
				}
			}
			for _, port := range []uint16{443, 22} {
				outgoing(port, 0x02)
				incoming(port, 0x12)
				outgoing(port, 0x18)
				incoming(port, 0x18)
			}
			fragment := func(port uint16, first bool) []byte {
				p := append(packet(false, port, 0x18), 0, 0, 0)
				binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
				binary.BigEndian.PutUint16(p[4:6], port)
				if first {
					binary.BigEndian.PutUint16(p[6:8], 0x2000)
				} else {
					binary.BigEndian.PutUint16(p[6:8], 3)
					clear(p[20:])
				}
				return p
			}
			for _, port := range []uint16{443, 22} {
				if err := s.send(fragment(port, true)); err != nil {
					t.Fatal("first fragment", port, err)
				}
				nextSwitchPrimary(t, s)
			}
			if err := s.send(packet(false, 443, 0x18)); err != nil {
				t.Fatal(err)
			}
			if event == "expiry" {
				f.lease.Unavailable()
				f.clock.advance(0, time.Second)
			} else {
				previous = f.verified.Cursor()
				f.clock.advance(time.Millisecond, time.Millisecond)
				peer.ACLGrants = peer.ACLGrants[1:]
				f.refresh(t, &previous)
				if err := s.Update(f.verified); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.send(packet(false, 443, 0x18)); !errors.Is(err, ErrNoAuthority) {
				t.Fatal("outbound stream survived", err)
			}
			if err := sink(packet(true, 443, 0x18)); !errors.Is(err, ErrNoAuthority) {
				t.Fatal("return stream survived", err)
			}
			if err := s.send(fragment(443, false)); !errors.Is(err, ErrNoAuthority) {
				t.Fatal("withdrawn fragment survived", err)
			}
			if err := s.send(fragment(22, false)); err != nil {
				t.Fatal("independent fragment lost", err)
			}
			if !bytes.Equal(nextSwitchPrimary(t, s).data, fragment(22, false)) {
				t.Fatal("unexpected fragment in queue")
			}
			outgoing(22, 0x18)
			incoming(22, 0x18)
		})
	}
}

func TestTCPHandshakeUsesInitiatorsGrant(t *testing.T) {
	expiry := time.Unix(1800000000, 0)
	g := windowsForPeer(coordinator.Peer{
		ACLGrants:        []coordinator.ACLGrant{{AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}},
		IngressACLGrants: []coordinator.ACLGrant{{AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 8443}}, ValidUntil: &expiry}},
	})
	for _, outbound := range []bool{true, false} {
		packet := make([]byte, 40)
		packet[0], packet[9], packet[32] = 0x45, 6, 0x50
		binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
		source, dest := uint16(8443), uint16(443)
		if !outbound {
			source, dest = dest, source
		}
		binary.BigEndian.PutUint16(packet[20:22], source)
		binary.BigEndian.PutUint16(packet[22:24], dest)
		for _, flags := range []byte{0x02, 0x12, 0x18} {
			packet[33] = flags
			until, ok := g.peerPacketWindow(packet, outbound)
			permanent := outbound && flags == 0x02 || !outbound && flags == 0x12
			if !ok || permanent && !until.IsZero() || !permanent && !until.Equal(expiry) {
				t.Fatal(outbound, flags, until, ok)
			}
		}
	}
}
