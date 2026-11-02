package dataplane

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
)

func TestDirectTrackerRequiresAuthenticatedTransportAtCurrentBinding(t *testing.T) {
	m, _ := engineMap(t)
	m.Peers[0].ID = "peer"
	tk := newDirectTracker()
	if err := tk.configure(m.Peers); err != nil {
		t.Fatal(err)
	}
	key := peerKey(t, m.Peers[0].PublicKey)
	initial := tk.config.Load()
	endpoint := initial.peers[[32]byte(key)].endpoint
	now := time.Now().UTC()
	tk.session(key, device.PeerSessionEstablished)
	paths, _, _ := tk.snapshot()
	if paths[0].Authenticated {
		t.Fatal("session key event stood in for delivered transport")
	}
	tk.authenticated([32]byte(key), netip.MustParseAddrPort("192.0.2.99:51820"), initial, now)
	paths, _, _ = tk.snapshot()
	if paths[0].Authenticated {
		t.Fatal("foreign endpoint confirmed")
	}
	tk.authenticated([32]byte{99}, endpoint, initial, now)
	tk.authenticated([32]byte(key), endpoint, initial, now)
	paths, _, _ = tk.snapshot()
	if !paths[0].Authenticated || paths[0].PeerID != "peer" {
		t.Fatal("valid transport not projected")
	}
	if err := tk.configure(m.Peers); err != nil {
		t.Fatal(err)
	}
	paths, _, _ = tk.snapshot()
	if !paths[0].Authenticated {
		t.Fatal("unchanged map destroyed established evidence")
	}
	m.Peers[0].Endpoint = "192.0.2.3:51820"
	if err := tk.configure(m.Peers); err != nil {
		t.Fatal(err)
	}
	tk.authenticated([32]byte(key), endpoint, initial, now.Add(time.Second))
	paths, _, _ = tk.snapshot()
	if paths[0].Authenticated {
		t.Fatal("late prior-endpoint packet confirmed current path")
	}
	current := tk.config.Load()
	remote := current.peers[[32]byte(key)].endpoint
	tk.authenticated([32]byte(key), remote, current, now.Add(time.Second))
	paths, _, _ = tk.snapshot()
	if !paths[0].Authenticated {
		t.Fatal("new endpoint transport missing")
	}
	tk.session(key, device.PeerSessionExpired)
	tk.authenticated([32]byte(key), remote, current, now.Add(2*time.Second))
	paths, _, _ = tk.snapshot()
	if paths[0].Authenticated {
		t.Fatal("queued callback restored expired keys")
	}
	if err := tk.configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := tk.configure(m.Peers); err != nil {
		t.Fatal(err)
	}
	tk.session(key, device.PeerSessionEstablished)
	tk.authenticated([32]byte(key), remote, current, now.Add(3*time.Second))
	paths, _, _ = tk.snapshot()
	if paths[0].Authenticated {
		t.Fatal("remove/readd adopted stale receive binding")
	}
	tk.close()
	tk.session(key, device.PeerSessionEstablished)
	paths, _, closed := tk.snapshot()
	if !closed || len(paths) != 0 {
		t.Fatal("closed tracker retained path")
	}
}

type memoryWirePacket struct {
	data   []byte
	source string
}
type memoryWireSocket struct {
	packets chan memoryWirePacket
	done    chan struct{}
}
type memoryWireBind struct {
	address          string
	peer             *memoryWireBind
	socket           atomic.Pointer[memoryWireSocket]
	dropTransport    atomic.Bool
	sent             [5]atomic.Uint64
	transportAttempt chan struct{}
}

func (b *memoryWireBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	s := &memoryWireSocket{packets: make(chan memoryWirePacket, 64), done: make(chan struct{})}
	if !b.socket.CompareAndSwap(nil, s) {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	return []conn.ReceiveFunc{func(slab []byte, packets []conn.ReceivedPacket) (int, error) {
		select {
		case <-s.done:
			return 0, net.ErrClosed
		case p := <-s.packets:
			if len(packets) == 0 || len(p.data) > len(slab) {
				return 0, ErrPacketIO
			}
			ep, err := b.ParseEndpoint(p.source)
			if err != nil {
				return 0, err
			}
			copy(slab, p.data)
			packets[0] = conn.ReceivedPacket{Size: len(p.data), Endpoint: ep}
			return 1, nil
		}
	}}, port, nil
}
func (b *memoryWireBind) Close() error {
	if s := b.socket.Swap(nil); s != nil {
		close(s.done)
	}
	return nil
}
func (*memoryWireBind) SetMark(uint32) error { return nil }
func (*memoryWireBind) BatchSize() int       { return 1 }
func (*memoryWireBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	return conn.NewStdNetBind().ParseEndpoint(s)
}
func (b *memoryWireBind) Send(bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	if _, wrapped := endpoint.(*directEndpoint); wrapped {
		return conn.ErrWrongEndpointType
	}
	if endpoint.DstToString() != b.peer.address {
		return ErrDirectPath
	}
	for _, buf := range bufs {
		if offset < 0 || offset > len(buf) {
			return ErrPacketIO
		}
		data := buf[offset:]
		if len(data) >= 4 {
			kind := binary.LittleEndian.Uint32(data[:4])
			if kind < 5 {
				b.sent[kind].Add(1)
			}
			if kind == device.MessageTransportType {
				select {
				case b.transportAttempt <- struct{}{}:
				default:
				}
			}
		}
		if b.dropTransport.Load() && len(data) >= 4 && binary.LittleEndian.Uint32(data[:4]) == device.MessageTransportType {
			continue
		}
		peer := b.peer.socket.Load()
		if peer == nil {
			continue
		}
		select {
		case peer.packets <- memoryWirePacket{data: bytes.Clone(data), source: b.address}:
		case <-peer.done:
		}
	}
	return nil
}

// This fixture uses actual upstream cryptography and asynchronous queues over
// bounded in-memory datagrams. Permission is deliberately injected for this
// transport test; it does not claim signed-map, WFP, TUN or application acceptance.
func directEnginePair(t *testing.T, firstBind, secondBind conn.Bind, firstEndpoint, secondEndpoint string) (*Engine, *Engine, *packetPermission) {
	t.Helper()
	firstMap, firstPrivate := engineMap(t)
	firstMap.Node.ID = "first"
	firstMap.Peers[0].ID = "second"
	firstMap.Peers[0].Endpoint = secondEndpoint
	secondMap := coordinator.NetworkMapSnapshot{Node: coordinator.Node{ID: "second", PublicKey: firstMap.Peers[0].PublicKey, AssignedIP: "100.64.0.2"}, Peers: []coordinator.Peer{{ID: "first", PublicKey: firstMap.Node.PublicKey, Endpoint: firstEndpoint, AllowedIPs: []string{"100.64.0.1/32"}, ACLRestricted: true}}}
	makeEngine := func(bind conn.Bind, endpoint string) (*Engine, *packetPermission) {
		port := netip.MustParseAddrPort(endpoint).Port()
		e, err := NewEngine(newInertTUN(), bind, 4, port, engineLease(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.Close)
		permission := &packetPermission{p: connectivity.Permission{NewFlows: true, ExistingFlows: true}}
		e.bind.permissions = permission.get
		return e, permission
	}
	first, permission := makeEngine(firstBind, firstEndpoint)
	second, _ := makeEngine(secondBind, secondEndpoint)
	if err := first.apply(context.Background(), firstMap, firstPrivate); err != nil {
		t.Fatal(err)
	}
	if err := second.apply(context.Background(), secondMap, bytes.Repeat([]byte{12}, 32)); err != nil {
		t.Fatal(err)
	}
	return first, second, permission
}

func waitDirectPair(t *testing.T, first, second *Engine, duration time.Duration) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- first.WaitDirect(ctx, 5*time.Millisecond) }()
	go func() { done <- second.WaitDirect(ctx, 5*time.Millisecond) }()
	return []error{<-done, <-done}
}

func TestActualWireGuardDirectNeedsTransportBeyondHandshakeKeys(t *testing.T) {
	firstBind := &memoryWireBind{address: "192.0.2.1:51820", transportAttempt: make(chan struct{}, 1)}
	secondBind := &memoryWireBind{address: "192.0.2.2:51820"}
	firstBind.peer, secondBind.peer = secondBind, firstBind
	firstBind.dropTransport.Store(true)
	secondBind.dropTransport.Store(true)
	first, second, permission := directEnginePair(t, firstBind, secondBind, firstBind.address, secondBind.address)
	initial, cancelInitial := context.WithTimeout(context.Background(), 2*time.Second)
	initialDone := make(chan error, 1)
	go func() { initialDone <- first.WaitDirect(initial, 5*time.Millisecond) }()
	select {
	case <-firstBind.transportAttempt:
	case <-initial.Done():
		t.Fatal("fixture did not produce authenticated transport after handshake")
	}
	cancelInitial()
	if err := <-initialDone; !errors.Is(err, context.Canceled) {
		t.Fatal("handshake key event completed path without transport delivery", err)
	}
	for _, e := range []*Engine{first, second} {
		paths, err := e.DirectPaths()
		if err != nil || len(paths) != 1 || paths[0].Authenticated {
			t.Fatal("dropped transport became path success", err)
		}
	}
	firstBind.dropTransport.Store(false)
	secondBind.dropTransport.Store(false)
	for _, err := range waitDirectPair(t, first, second, time.Second) {
		if err != nil {
			t.Fatal("actual authenticated keepalives did not confirm Direct", err, "first initiation/response/transport", firstBind.sent[1].Load(), firstBind.sent[2].Load(), firstBind.sent[4].Load(), "second", secondBind.sent[1].Load(), secondBind.sent[2].Load(), secondBind.sent[4].Load())
		}
	}
	for _, e := range []*Engine{first, second} {
		paths, err := e.DirectPaths()
		if err != nil || len(paths) != 1 || !paths[0].Authenticated || paths[0].ConfirmedAt.IsZero() {
			t.Fatal("actual proof missing", err)
		}
	}
	permission.set(connectivity.Permission{ExistingFlows: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := first.WaitDirect(ctx, time.Millisecond); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("offline existing-flow permission completed new Connect", err)
	}
	first.Close()
	if _, err := first.DirectPaths(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed device kept connected path", err)
	}
}
