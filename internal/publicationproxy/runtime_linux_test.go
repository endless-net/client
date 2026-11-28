package publicationproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	contract "github.com/endless-net/coordinator/api/enrollment"
	api "github.com/endless-net/management/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProxyConfiguration(t *testing.T) {
	const valid = `{"public_address":"192.0.2.5:443","account_id":"a","network_id":"n","node_id":"node","proxy_id":"proxy","management_origin":"https://management:443","acme_directory":"https://ca/directory","accepted_terms_url":"https://ca/terms"}`
	if _, err := decodeConfig([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		strings.Replace(valid, "192.0.2.5", "0.0.0.0", 1),
		strings.Replace(valid, "https://management", "http://management", 1),
		strings.Replace(valid, "management:443", "management:443/path", 1),
		strings.Replace(valid, "https://ca/terms", "", 1), valid + "{}", strings.Replace(valid, "node_id", "node", 1),
	} {
		if _, err := decodeConfig([]byte(s)); err == nil {
			t.Fatal(s)
		}
	}
}

type offeredListener struct {
	in   chan net.Conn
	done chan struct{}
}

func (l *offeredListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.in:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *offeredListener) Addr() net.Addr { return &net.TCPAddr{} }
func (l *offeredListener) Close() error   { close(l.done); return nil }

type sourceConn struct {
	net.Conn
	addr *net.TCPAddr
}

func (c sourceConn) RemoteAddr() net.Addr { return c.addr }

func TestOnlyDeviceBoundListenerAttestsSource(t *testing.T) {
	public := &offeredListener{make(chan net.Conn, 1), make(chan struct{})}
	overlay := &offeredListener{make(chan net.Conn, 1), make(chan struct{})}
	peer := &api.PublicationPeer{NodeId: "node", PublicKey: "key"}
	both := combineListeners(public, overlay, map[string]*api.PublicationPeer{"100.70.0.2": peer})
	defer func() { _ = both.Close() }()
	offer := func(l *offeredListener, ip string) net.Conn {
		a, b := net.Pipe()
		t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
		l.in <- sourceConn{a, &net.TCPAddr{IP: net.ParseIP(ip), Port: 443}}
		return b
	}
	for _, l := range []*offeredListener{public, overlay} {
		offer(l, "100.70.0.2")
		c, err := both.Accept()
		if err != nil {
			t.Fatal(err)
		}
		got := authenticatedPeer(tls.Server(c, &tls.Config{MinVersion: tls.VersionTLS13}))
		if (l == public && got != nil) || (l == overlay && got != peer) {
			t.Fatal(got)
		}
	}
	b := offer(overlay, "100.70.0.7")
	_ = b.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := b.Read(one[:]); err == nil {
		t.Fatal("unrecognized peer admitted")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("unknown peer socket retained")
	}
	if err := both.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := both.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestPublicationWorkerStopsAtSessionBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	w := &LinuxWorker{done: make(chan struct{}), cancel: cancel}
	go func() { <-ctx.Done(); w.err = ctx.Err(); close(w.done) }()
	if w.Err() != nil {
		t.Fatal("premature stop")
	}
	if err := w.Stop(t.Context()); err != nil || !errors.Is(w.Err(), context.Canceled) {
		t.Fatal(err, w.Err())
	}
	if err := bindOverlay(0)("", "", nil); !errors.Is(err, connectivity.ErrConfig) {
		t.Fatal("unbound dial", err)
	}
}

func TestOfflineProxyReportsOnlyDrainedEmptyGeneration(t *testing.T) {
	e := &api.PublicationExecutor{AccountId: "a", NetworkId: "n", ProxyId: "p", NodeId: "node"}
	now := time.Now()
	control := &publicationControl{s: &api.GetPublicationRoutesResponse{Executor: e, Generation: 3, LeaseId: "three", IssuedAt: timestamppb.New(now.Add(-time.Second)), ValidUntil: timestamppb.New(now.Add(time.Minute)), Routes: []*api.PublicationRoute{{ServiceId: "docs"}}}}
	p := &LinuxRuntime{Executor: e, Management: control}
	if err := p.reportIdle(t.Context()); err != nil || len(control.reports) != 0 {
		t.Fatal("offline listener claimed live route", err)
	}
	control.s.Routes = nil
	p.worker = &LinuxWorker{done: make(chan struct{})}
	if err := p.reportIdle(t.Context()); err != nil || len(control.reports) != 0 {
		t.Fatal("overlapped serving worker", err)
	}
	p.worker.err = context.DeadlineExceeded
	close(p.worker.done)
	if err := p.reportIdle(t.Context()); err != nil || len(control.reports) != 0 {
		t.Fatal("incomplete drain acknowledged", err)
	}
	p.worker = nil // fresh exclusive process
	control.lost = true
	if err := p.reportIdle(t.Context()); err == nil {
		t.Fatal("lost reply")
	}
	if err := p.reportIdle(t.Context()); err != nil || len(control.reports) != 2 {
		t.Fatal(err)
	}
	for _, r := range control.reports {
		if r.Generation != 3 || r.LeaseId != "three" || len(r.Observations) != 0 {
			t.Fatal(r)
		}
	}
}

func TestPublicationPeerSocketGeneration(t *testing.T) {
	w := LinuxWorker{peers: map[string]*api.PublicationPeer{"100.70.0.2": {NodeId: "n", PublicKey: "k"}}}
	p := contract.Peer{ID: "n", PublicKey: "k", AllowedIPs: []string{"100.70.0.2/32"}}
	if !w.matchesPeers([]contract.Peer{p}) {
		t.Fatal("unchanged owner")
	}
	for _, change := range []func(*contract.Peer){
		func(p *contract.Peer) { p.ID = "other" }, func(p *contract.Peer) { p.PublicKey = "new" }, func(p *contract.Peer) { p.AllowedIPs = []string{"100.70.0.3/32"} },
	} {
		changed := p
		change(&changed)
		if w.matchesPeers([]contract.Peer{changed}) {
			t.Fatal("socket identity survived reassignment")
		}
	}
	if w.matchesPeers(nil) {
		t.Fatal("socket identity survived removal")
	}
}
