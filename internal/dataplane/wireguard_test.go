package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
)

func engineMap(t *testing.T) (coordinator.NetworkMapSnapshot, []byte) {
	t.Helper()
	private := bytes.Repeat([]byte{11}, 32)
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{12}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return coordinator.NetworkMapSnapshot{Node: coordinator.Node{PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), AssignedIP: "100.64.0.1"}, Peers: []coordinator.Peer{{PublicKey: base64.StdEncoding.EncodeToString(peer.PublicKey().Bytes()), Endpoint: "192.0.2.2:51820", AllowedIPs: []string{"100.64.0.2/32"}, ACLRestricted: true}}}, private
}

// This TUN has no OS handles, routes or driver. It lets the actual upstream
// device run its lifecycle while producing no packets and no adapter events.
type inertTUN struct {
	done   chan struct{}
	events chan tun.Event
	once   sync.Once
}

func newInertTUN() *inertTUN {
	return &inertTUN{done: make(chan struct{}), events: make(chan tun.Event)}
}
func (*inertTUN) File() *os.File                               { return nil }
func (t *inertTUN) Read([]byte, []tun.ReadPacket) (int, error) { <-t.done; return 0, os.ErrClosed }
func (*inertTUN) Write(bufs [][]byte, _ int) (int, error)      { return len(bufs), nil }
func (*inertTUN) MTU() (int, error)                            { return 1280, nil }
func (*inertTUN) Name() (string, error)                        { return "unit-no-os", nil }
func (t *inertTUN) Events() <-chan tun.Event                   { return t.events }
func (*inertTUN) BatchSize() int                               { return 1 }
func (t *inertTUN) Close() error {
	t.once.Do(func() { close(t.done); close(t.events) })
	return nil
}

type inertBind struct {
	mu   sync.Mutex
	done chan struct{}
}

func (b *inertBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	done := make(chan struct{})
	b.done = done
	return []conn.ReceiveFunc{func([]byte, []conn.ReceivedPacket) (int, error) { <-done; return 0, net.ErrClosed }}, port, nil
}
func (b *inertBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
	return nil
}
func (*inertBind) SetMark(uint32) error                    { return nil }
func (*inertBind) Send([][]byte, conn.Endpoint, int) error { return nil }
func (*inertBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	return conn.NewStdNetBind().ParseEndpoint(s)
}
func (*inertBind) BatchSize() int { return 1 }

func peerKey(t *testing.T, encoded string) (key device.NoisePublicKey) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != len(key) {
		t.Fatal("bad fixture key")
	}
	copy(key[:], raw)
	return key
}

type engineClock struct{}

func (engineClock) Sample() (connectivity.ClockSample, error) {
	return connectivity.ClockSample{Wall: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "unit-engine"}, nil
}

func engineLease(t *testing.T) *connectivity.Lease {
	t.Helper()
	l, err := connectivity.NewLease(context.Background(), engineClock{}, "unit-profile", 1)
	if err != nil {
		t.Fatal(err)
	}
	return l // starts with no authority; lifecycle tests produce no packets
}

func TestActualWireGuardEngineUpdatesRetainAndRemovePeers(t *testing.T) {
	m, private := engineMap(t)
	tunnel := newInertTUN()
	e, err := NewEngine(tunnel, &inertBind{}, 4, 51820, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	if err := e.apply(context.Background(), m, private); err != nil {
		t.Fatal(err)
	}
	d := e.device.(*device.Device)
	key := peerKey(t, m.Peers[0].PublicKey)
	before := d.LookupPeer(key)
	if before == nil {
		t.Fatal("actual engine did not install peer")
	}
	m.Peers[0].Endpoint = "192.0.2.3:51821"
	if err := e.apply(context.Background(), m, private); err != nil {
		t.Fatal(err)
	}
	if d.LookupPeer(key) != before {
		t.Fatal("map refresh replaced peer and discarded its session")
	}
	m.Peers = nil
	if err := e.apply(context.Background(), m, private); err != nil || d.LookupPeer(key) != nil {
		t.Fatalf("peer removal failed: %v", err)
	}
	e.Close()
	e.Close()
	select {
	case <-tunnel.done:
	default:
		t.Fatal("engine did not close owned TUN")
	}
	select {
	case <-d.Wait():
	default:
		t.Fatal("actual device workers not stopped")
	}
	if err := e.apply(context.Background(), m, private); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestWireGuardRejectsUnsafeProjectionBeforeUAPI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*coordinator.NetworkMapSnapshot, *[]byte)
	}{
		{"default route", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].AllowedIPs[0] = "0.0.0.0/0" }},
		{"subnet", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].AllowedIPs[0] = "100.64.0.0/24" }},
		{"ipv6", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].AllowedIPs[0] = "2001:db8::2/128" }},
		{"own address", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].AllowedIPs[0] = "100.64.0.1/32" }},
		{"duplicate", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers = append(m.Peers, m.Peers[0]) }},
		{"hostname endpoint", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].Endpoint = "example.com:51820" }},
		{"injected endpoint", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].Endpoint += "\nreplace_peers=true" }},
		{"unrestricted", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Peers[0].ACLRestricted = false }},
		{"foreign key", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Node.PublicKey = m.Peers[0].PublicKey }},
		{"zero private", func(_ *coordinator.NetworkMapSnapshot, p *[]byte) { *p = make([]byte, 32) }},
		{"revoked", func(m *coordinator.NetworkMapSnapshot, _ *[]byte) { m.Node.Status = "revoked" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, private := engineMap(t)
			tc.change(&m, &private)
			d := &failingDevice{}
			e := &Engine{device: d, maxPeers: 4, listenPort: 51820, direct: newDirectTracker()}
			if err := e.apply(context.Background(), m, private); !errors.Is(err, ErrConfiguration) || d.calls != 0 || d.closed != 1 {
				t.Fatalf("unsafe update: %v calls%d closed%d", err, d.calls, d.closed)
			}
		})
	}
}

type failingDevice struct {
	calls, closed int
	upError       bool
	setError      bool
	cancel        context.CancelFunc
}

func (d *failingDevice) IpcSetOperation(r io.Reader) error {
	d.calls++
	_, _ = io.Copy(io.Discard, r)
	if d.cancel != nil {
		d.cancel()
	}
	if d.setError {
		return errors.New("raw UAPI private_key=must-not-escape")
	}
	return nil
}
func (d *failingDevice) Up() error {
	if d.upError {
		return errors.New("raw bind failure")
	}
	return nil
}
func (d *failingDevice) Close() { d.closed++ }

func TestWireGuardFailureAndCancellationCloseWithoutRawDiagnostics(t *testing.T) {
	for _, which := range []string{"set", "up", "cancel-before", "cancel-after-set", "zero-map"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := &failingDevice{setError: which == "set", upError: which == "up"}
			if which == "cancel-before" {
				cancel()
			}
			if which == "cancel-after-set" {
				d.cancel = cancel
			}
			e := &Engine{device: d, maxPeers: 4, listenPort: 51820, direct: newDirectTracker()}
			m, private := engineMap(t)
			var err error
			if which == "zero-map" {
				err = e.Apply(ctx, connectivity.Map{}, private)
			} else {
				err = e.apply(ctx, m, private)
			}
			if err == nil || d.closed != 1 || strings.Contains(err.Error(), "raw") || strings.Contains(err.Error(), "private_key") {
				t.Fatalf("failure escaped: %v close%d", err, d.closed)
			}
			if which == "cancel-before" && d.calls != 0 {
				t.Fatal("cancelled call mutated engine")
			}
			for _, rendered := range []string{fmt.Sprint(e), fmt.Sprintf("%#v", e)} {
				if rendered != "[WireGuard engine]" {
					t.Fatal("unsafe engine formatting")
				}
			}
		})
	}
}

func TestWireGuardRefreshDoesNotResendPrivateKeyOrReplaceAllPeers(t *testing.T) {
	m, private := engineMap(t)
	first, public, peers, err := configuration(m, private, "", nil, 4, 51820)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(first)
	if !bytes.Contains(first, []byte("private_key=")) || !bytes.Contains(first, []byte("replace_peers=true")) || !bytes.Contains(first, []byte("listen_port=51820\n")) {
		t.Fatal("initial engine configuration incomplete")
	}
	refresh, _, _, err := configuration(m, private, public, peers, 4, 51820)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(refresh)
	if bytes.Contains(refresh, []byte("private_key=")) || bytes.Contains(refresh, []byte("replace_peers=")) || bytes.Contains(refresh, []byte("listen_port=")) {
		t.Fatal("refresh replaced device identity or all sessions")
	}
	m.Peers = nil
	removed, _, _, err := configuration(m, private, public, peers, 4, 51820)
	if err != nil || !bytes.Contains(removed, []byte("remove=true")) {
		t.Fatal("removed peer retained", err)
	}
	clear(removed)
}

func TestWireGuardConcurrentApplyAndCloseNeverReopens(t *testing.T) {
	m, private := engineMap(t)
	e, err := NewEngine(newInertTUN(), &inertBind{}, 4, 51820, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			for range 8 {
				if err := e.apply(context.Background(), m, private); err != nil && !errors.Is(err, ErrClosed) {
					t.Error(err)
				}
			}
		})
	}
	wg.Go(e.Close)
	wg.Wait()
	if err := e.apply(context.Background(), m, private); !errors.Is(err, ErrClosed) {
		t.Fatal("late apply reopened a stopped engine", err)
	}
}
