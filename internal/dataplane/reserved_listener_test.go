package dataplane

import (
	"net"
	"testing"
	"time"
)

func TestReservedListenerHandoffAndWithdrawal(t *testing.T) {
	f := newCandidateFixture(t)
	snap, err := f.verified.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	peer := snap.Peers[0]
	offer := peer.Transport.Profiles[0]
	p := &supervisedRelay{}
	g := &transportSupervisor{clock: f.clock, lease: f.lease, current: f.verified, snapshot: snap, applied: true, generation: 1, config: supervisorConfig{reserveListener: reserveUDPListener}, peers: map[string]*supervisedRelay{peer.ID: p}}
	t.Cleanup(func() {
		for _, l := range p.listeners {
			l.close()
		}
	})
	g.publishListeners()
	l := p.listeners[offer.Profile.ID]
	if l == nil || l.socket == nil || l.port == 0 {
		t.Fatal("no reserved port for the signed profile")
	}
	port := l.port
	if p.takeListener(offer) != port {
		t.Fatal("prepared port was lost before candidate handoff")
	}
	conn, err := reserveUDPListener(port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A different local process claimed the port; only a newly owned socket may be published.
	g.publishListeners()
	if l.socket == nil || l.port == port || len(g.preparedListeners()) == 0 {
		t.Fatal("did not publish the replacement listener")
	}
	replacement := l.port
	g.snapshot.Peers[0].Transport = nil
	g.publishListeners()
	if len(p.listeners) != 0 || len(g.preparedListeners()) != 0 {
		t.Fatal("withdrawn profile is still advertised")
	}
	reopened, err := net.ListenUDP("udp4", &net.UDPAddr{Port: int(replacement)})
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
}

func TestCandidateReopensReservedPortWithoutQueuedTraffic(t *testing.T) {
	f := newCandidateFixture(t)
	b, err := newCandidateBind(t.Context(), f.assignment(t), f.clock)
	if err != nil {
		t.Fatal(err)
	}
	defer b.shutdown()
	idle, err := reserveUDPListener(0)
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(idle.LocalAddr().(*net.UDPAddr).Port)
	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)})
	if err != nil {
		idle.Close()
		t.Fatal(err)
	}
	defer sender.Close()
	if _, err = sender.Write([]byte("old")); err != nil {
		idle.Close()
		t.Fatal(err)
	}
	idle.Close()
	b.listen = func(port uint16) (candidateSocket, error) {
		return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)})
	}
	b.preferredPort = port
	_, got, err := b.Open(0)
	if err != nil || got != port {
		t.Fatalf("opened %d, want %d: %v", got, port, err)
	}
	if _, err = sender.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	var buf [32]byte
	socket := b.socket.(*net.UDPConn)
	if err := socket.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := socket.ReadFromUDPAddrPort(buf[:])
	if err != nil || string(buf[:n]) != "new" {
		t.Fatalf("fresh socket got %q: %v", buf[:n], err)
	}
}

func TestReservedListenerDuringSourcePause(t *testing.T) {
	for _, action := range []string{"resume", "expire", "plan_expire", "removed_peer", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := newCandidateFixture(t)
			if action == "plan_expire" {
				f.planTTL = 5 * time.Second
				cursor := f.verified.Cursor()
				f.clock.advance(time.Millisecond, time.Millisecond)
				f.refresh(t, &cursor)
			}
			snap, err := f.verified.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			peer := snap.Peers[0]
			p := &supervisedRelay{}
			g := &transportSupervisor{clock: f.clock, lease: f.lease, current: f.verified, snapshot: snap, applied: true, generation: 1, config: supervisorConfig{reserveListener: reserveUDPListener}, peers: map[string]*supervisedRelay{peer.ID: p}}
			g.publishListeners()
			listener := p.listeners[peer.Transport.Profiles[0].Profile.ID]
			if listener == nil || listener.socket == nil {
				t.Fatal("missing initial reservation")
			}
			defer listener.close()
			port := listener.port
			f.lease.PausePackets(true)
			g.publishListeners()
			if listener.socket == nil {
				t.Fatal("lost an authorized port during a temporary pause")
			}
			switch action {
			case "resume":
				f.lease.PausePackets(false)
				g.publishListeners()
				if listener.socket == nil || port != listener.port {
					t.Fatal("did not retain authorized listener")
				}
				return
			case "expire":
				f.clock.advance(2*time.Minute, 2*time.Minute)
			case "plan_expire":
				f.clock.advance(6*time.Second, 6*time.Second)
			case "removed_peer":
				g.snapshot.Peers = nil
			case "revoke":
				f.lease.Revoke()
			}
			g.publishListeners()
			if len(g.listeners) != 0 || len(p.listeners) != 0 {
				t.Fatal("obsolete listener retained after " + action)
			}
			conn, err := reserveUDPListener(port)
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
		})
	}
}
