package dataplane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

type candidateDatagram struct {
	raw     []byte
	address netip.AddrPort
}
type candidateSocketFixture struct {
	mu             sync.Mutex
	reads          chan candidateDatagram
	done           chan struct{}
	once           sync.Once
	writes         []candidateDatagram
	deadline       time.Time
	beforeDeadline func()
	wrote          chan struct{}
	forward        func(candidateDatagram) error
}

func newCandidateSocketFixture() *candidateSocketFixture {
	return &candidateSocketFixture{reads: make(chan candidateDatagram, 4), done: make(chan struct{}), wrote: make(chan struct{}, 16)}
}
func (s *candidateSocketFixture) ReadFromUDPAddrPort(raw []byte) (int, netip.AddrPort, error) {
	select {
	case <-s.done:
		return 0, netip.AddrPort{}, net.ErrClosed
	case p := <-s.reads:
		return copy(raw, p.raw), p.address, nil
	}
}
func (s *candidateSocketFixture) WriteToUDPAddrPort(raw []byte, address netip.AddrPort) (int, error) {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return 0, net.ErrClosed
	default:
	}
	s.writes = append(s.writes, candidateDatagram{bytes.Clone(raw), address})
	forward := s.forward
	s.mu.Unlock()
	select {
	case s.wrote <- struct{}{}:
	default:
	}
	if forward != nil {
		if err := forward(candidateDatagram{bytes.Clone(raw), address}); err != nil {
			return 0, err
		}
	}
	return len(raw), nil
}
func (s *candidateSocketFixture) SetWriteDeadline(deadline time.Time) error {
	s.mu.Lock()
	hook := s.beforeDeadline
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	s.mu.Lock()
	s.deadline = deadline
	s.mu.Unlock()
	return nil
}
func (s *candidateSocketFixture) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4zero, Port: 51821}
}
func (s *candidateSocketFixture) Close() error { s.once.Do(func() { close(s.done) }); return nil }

func TestCandidateSendGuardRunsAfterGateAndImmediatelyBeforeSocket(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(confirmed), func(t *testing.T) {
			b, _, socket, _, endpoint := candidateBindFixture(t)
			b.mu.Lock()
			b.receiveConfirmed = confirmed
			b.mu.Unlock()
			<-b.sendGate
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			called := make(chan int, 1)
			finished := make(chan error, 1)
			go func() {
				finished <- b.SendGuarded([][]byte{{1, 2, 3}}, endpoint, []func(int) error{func(size int) error {
					called <- size
					return ctx.Err()
				}})
			}()
			select {
			case <-called:
				t.Fatal("guard evaluated before socket gate")
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			b.sendGate <- struct{}{}
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("queued guard ignored cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("guarded send stuck")
			}
			if size := <-called; size != 3 {
				t.Fatal("guard did not receive ciphertext length", size)
			}
			socket.mu.Lock()
			writes := 0
			for _, packet := range socket.writes {
				if bytes.Equal(packet.raw, []byte{1, 2, 3}) {
					writes++
				}
			}
			socket.mu.Unlock()
			if writes != 0 {
				t.Fatal("canceled service packet reached socket")
			}
			if err := b.Send([][]byte{{4}}, endpoint); err != nil {
				t.Fatal("service cancellation stopped ordinary ciphertext", err)
			}
		})
	}
}

func candidateBindFixture(t *testing.T) (*candidateBind, *candidateFixture, *candidateSocketFixture, *probeAttempt, awgconn.Endpoint) {
	t.Helper()
	f := newCandidateFixture(t)
	b, err := newCandidateBind(t.Context(), f.assignment(t), f.clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.shutdown)
	socket := newCandidateSocketFixture()
	b.listen = func(uint16) (candidateSocket, error) { return socket, nil }
	_, port, err := b.Open(0)
	if err != nil || port != 51821 {
		t.Fatal("listener did not acknowledge actual port", port, err)
	}
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	attempt, err := cycle.attempt("nonce")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.beginProbe(attempt); err != nil {
		t.Fatal(err)
	}
	endpoint, err := b.ParseEndpoint("192.0.2.20:51820")
	if err != nil {
		t.Fatal(err)
	}
	return b, f, socket, attempt, endpoint
}

func TestCandidateAssignmentRequiresSealedCurrentPairAndProtectedIntent(t *testing.T) {
	f := newCandidateFixture(t)
	now, _ := f.clock.Sample()
	if _, err := newCandidateAssignment(connectivity.Map{}, f.lease, "peer", "fixture-profile", now.Wall); err == nil {
		t.Fatal("unsigned map accepted")
	}
	for _, ids := range [][2]string{{"foreign", "fixture-profile"}, {"peer", "foreign"}} {
		if _, err := newCandidateAssignment(f.verified, f.lease, ids[0], ids[1], now.Wall); err == nil {
			t.Fatal("unassigned pair/profile accepted")
		}
	}
	old := f.assignment(t)
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if err := old.permission(now.Wall, true); err == nil {
		t.Fatal("previous signed payload accepted after a current map was confirmed")
	}
	current := f.assignment(t)
	if !old.sameProbe(current) {
		t.Fatal("same-revision signature renewal reset probe")
	}
	f.snapshot.Peers[0].Transport.LocalIntentGeneration = 2
	f.snapshot.Node.Transport.IntentGeneration = 2
	cursor = f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	sample, _ := f.clock.Sample()
	if _, err := newCandidateAssignment(f.verified, f.lease, "peer", "fixture-profile", sample.Wall); err == nil {
		t.Fatal("remote local intent claim replaced protected intent")
	}
}

func TestCandidateBindPreludeEndpointByteAndDeadlineBoundaries(t *testing.T) {
	b, f, socket, attempt, endpoint := candidateBindFixture(t)
	for _, address := range []string{"host.example:51820", "192.0.2.20:51821", "192.0.2.21:51820", "[2001:db8::1]:51820"} {
		if _, err := b.ParseEndpoint(address); err == nil {
			t.Fatal("foreign/guessed endpoint accepted", address)
		}
	}
	if err := b.Send([][]byte{{9, 8, 7}}, endpoint); err != nil {
		t.Fatal(err)
	}
	if err := b.Send([][]byte{{6}}, endpoint); err != nil {
		t.Fatal(err)
	}
	socket.mu.Lock()
	writes := append([]candidateDatagram(nil), socket.writes...)
	deadline := socket.deadline
	socket.mu.Unlock()
	if len(writes) != 3 || len(writes[0].raw) != 6 || !bytes.Equal(writes[0].raw[:2], []byte{1, 2}) || !bytes.Equal(writes[1].raw, []byte{9, 8, 7}) || !bytes.Equal(writes[2].raw, []byte{6}) {
		t.Fatal("prelude reordered or replayed")
	}
	for _, write := range writes {
		if write.address.String() != "192.0.2.20:51820" {
			t.Fatal("endpoint authority lost")
		}
	}
	sample, _ := f.clock.Sample()
	if deadline.After(sample.Wall.Add(3 * time.Second)) {
		t.Fatal("UDP write exceeded original attempt")
	}
	if attempt.cycle.wire.used != 6+3+1+3*28 {
		t.Fatal("handshake/prelude/header bytes not charged", attempt.cycle.wire.used)
	}
	if err := attempt.cycle.charge(transport.MaxProbeBytes - attempt.cycle.wire.used); err != nil {
		t.Fatal(err)
	}
	if err := b.Send([][]byte{{1}}, endpoint); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("exhausted budget sent bytes", err)
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if len(socket.writes) != 3 {
		t.Fatal("exhausted candidate emitted traffic")
	}
}

func TestCandidateBindDropsWrongSourceAndExpiredProbeBeforeEngine(t *testing.T) {
	b, f, socket, attempt, _ := candidateBindFixture(t)
	raw := make([]byte, 128)
	sizes := []int{0}
	endpoints := make([]awgconn.Endpoint, 1)
	socket.reads <- candidateDatagram{[]byte{1, 2, 3}, netip.MustParseAddrPort("192.0.2.30:51820")}
	if n, err := b.receive(socket, [][]byte{raw}, sizes, endpoints); n != 0 || err != nil || attempt.cycle.wire.used != 0 {
		t.Fatal("foreign UDP source reached engine", n, err)
	}
	socket.reads <- candidateDatagram{[]byte{4, 5, 6}, netip.MustParseAddrPort("192.0.2.20:51820")}
	if n, err := b.receive(socket, [][]byte{raw}, sizes, endpoints); n != 1 || err != nil || attempt.cycle.wire.used != 31 || sizes[0] != 3 {
		t.Fatal("signed source not admitted/accounted", n, err)
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	socket.reads <- candidateDatagram{[]byte{7, 8, 9}, netip.MustParseAddrPort("192.0.2.20:51820")}
	if n, err := b.receive(socket, [][]byte{raw}, sizes, endpoints); n != 0 || err != nil || !bytes.Equal(raw[:3], make([]byte, 3)) {
		t.Fatal("expired probe plaintext/ciphertext retained", n, err)
	}
}

func TestCandidateBindRechecksAuthorityAfterDeadlineIOAndPreservesListenerOnRenewal(t *testing.T) {
	b, f, socket, attempt, endpoint := candidateBindFixture(t)
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if err := b.Send([][]byte{{1}}, endpoint); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("stale map still sent", err)
	}
	if err := b.update(f.assignment(t)); err != nil {
		t.Fatal(err)
	}
	if b.probe != attempt || b.socket != socket {
		t.Fatal("signature renewal discarded original attempt/listener")
	}
	socket.beforeDeadline = func() { f.clock.advance(3*time.Second, 3*time.Second) }
	if err := b.Send([][]byte{{1}}, endpoint); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("late UDP effect ignored expired probe", err)
	}
	socket.mu.Lock()
	if len(socket.writes) != 0 {
		t.Fatal("expired prelude emitted after blocking boundary")
	}
	socket.mu.Unlock()
}

func TestCandidateBindCloseOpenContractAndCancellation(t *testing.T) {
	b, _, socket, _, _ := candidateBindFixture(t)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-socket.done:
	default:
		t.Fatal("Close did not unblock receiver")
	}
	next := newCandidateSocketFixture()
	b.listen = func(uint16) (candidateSocket, error) { return next, nil }
	if _, _, err := b.Open(0); err != nil {
		t.Fatal("upstream Close/Open lifecycle rejected", err)
	}
	b.shutdown()
	if _, _, err := b.Open(0); !errors.Is(err, net.ErrClosed) {
		t.Fatal("owner cancellation reopened bind", err)
	}
}

func TestActualAWGUsesCandidateBindCloseOpenAndBoundedPrelude(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.assignment(t)
	b, err := newCandidateBind(t.Context(), a, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.shutdown)
	socket := newCandidateSocketFixture()
	b.listen = func(uint16) (candidateSocket, error) { return socket, nil }
	port, err := newPacketPort(t.Context(), 1280, f.lease.Permissions, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	peer := a.peer
	peer.Endpoint = a.offer.PeerEndpoint
	cipher, err := newPeerCipher(t.Context(), a.local, peer, f.profile.Keys.WireGuardPrivate, a.offer.Profile, port, b)
	if err != nil {
		_ = port.Close()
		t.Fatal("actual AWG Up failed to reopen bind", err)
	}
	t.Cleanup(cipher.Close)
	if actual, err := cipher.BoundPort(); err != nil || actual != 51821 {
		t.Fatal("cipher did not retain actual UDP listener", actual, err)
	}
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	attempt, err := cycle.attempt("nonce")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.beginProbe(attempt); err != nil {
		t.Fatal(err)
	}
	if err := cipher.Queue(cipherIPv4(a.local.AssignedIP, "100.70.0.2", 1), true); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for range 2 {
		select {
		case <-socket.wrote:
		case <-deadline.C:
			t.Fatal("actual AWG did not emit prelude and handshake")
		}
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if len(socket.writes) < 2 || len(socket.writes[0].raw) != 6 || len(socket.writes[1].raw) <= 6 {
		t.Fatal("actual encrypted handshake overtook prelude")
	}
}

func TestCandidateHandshakeRetransmitKeepsWireBudgetPreludeAndOriginalDeadline(t *testing.T) {
	b, f, socket, attempt, endpoint := candidateBindFixture(t)
	// Bind policy fixture; actual WG/AWG classification has crypto roundtrips.
	b.handshake = func(raw []byte) bool { return bytes.Equal(raw, []byte{7, 8, 9}) }
	raw := []byte{7, 8, 9}
	if err := b.Send([][]byte{raw}, endpoint); err != nil {
		t.Fatal(err)
	}
	socket.mu.Lock()
	before := len(socket.writes)
	socket.mu.Unlock()
	used := attempt.cycle.wire.used
	f.clock.advance(time.Second, time.Second)
	if err := b.resendHandshake(attempt); err != nil {
		t.Fatal(err)
	}
	socket.mu.Lock()
	if len(socket.writes) != before+1 || !bytes.Equal(socket.writes[before].raw, raw) {
		t.Error("retry changed the upstream frame or replayed the prelude")
	}
	socket.mu.Unlock()
	if attempt.cycle.wire.used != used+len(raw)+28 {
		t.Fatal("handshake retry escaped the candidate wire byte budget")
	}
	attempt.cycle.wire.mu.Lock()
	attempt.cycle.wire.used = transport.MaxProbeBytes
	attempt.cycle.wire.mu.Unlock()
	if err := b.resendHandshake(attempt); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("retry exceeded the original candidate byte budget", err)
	}
	f.clock.advance(2*time.Second, 2*time.Second)
	if err := b.resendHandshake(attempt); err == nil {
		t.Fatal("retry renewed the original three-second attempt")
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if len(socket.writes) != before+1 {
		t.Fatal("denied retry emitted a datagram")
	}
}

func TestCandidateBindGrantChangeInvalidatesProbeWithoutReplacingSocket(t *testing.T) {
	b, f, socket, attempt, endpoint := candidateBindFixture(t)
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	f.refresh(t, &cursor)
	if err := b.update(f.assignment(t)); err != nil {
		t.Fatal(err)
	}
	if b.probe != nil || b.socket != socket {
		t.Fatal("revision retained nonce or discarded prepared listener")
	}
	if err := b.Send([][]byte{{1}}, endpoint); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("old revision sent bytes", err)
	}
	if err := attempt.check(); !errors.Is(err, context.Canceled) {
		t.Fatal("old revision did not cancel its attempt", err)
	}
	if err := b.beginProbe(attempt); err == nil {
		t.Fatal("old nonce rebound to a new revision", err)
	}
}

func TestCandidateBindCannotProbeUntilSignedLocalPortMatchesListener(t *testing.T) {
	b, f, _, attempt, _ := candidateBindFixture(t)
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Node.Transport.Endpoints[0].Endpoint = "192.0.2.10:51822"
	f.snapshot.Peers[0].Transport.Profiles[0].LocalEndpoint = "192.0.2.10:51822"
	f.refresh(t, &cursor)
	if err := b.update(f.assignment(t)); err != nil {
		t.Fatal(err)
	}
	next, err := attempt.cycle.attempt("fresh-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.beginProbe(next); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("guessed/unannounced local port admitted probe", err)
	}
}

func TestCandidateBindOldNonceCannotReplaceNewerAttempt(t *testing.T) {
	b, _, _, first, _ := candidateBindFixture(t)
	second, err := first.cycle.attempt("next-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.beginProbe(second); err != nil {
		t.Fatal(err)
	}
	if err := b.beginProbe(first); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("old attempt replaced newer nonce", err)
	}
}
