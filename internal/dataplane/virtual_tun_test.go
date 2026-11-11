package dataplane

import (
	"bytes"
	"context"
	"net"
	"testing"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	wgtun "github.com/tailscale/wireguard-go/tun"
)

func TestVirtualPortCopiesQueuesAndDropsNewControlWhileOffline(t *testing.T) {
	permission := &packetPermission{p: connectivity.Permission{NewFlows: true, ExistingFlows: true}}
	port, err := newPacketPort(t.Context(), 1420, permission.get, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := port.Close(); err != nil {
			t.Error(err)
		}
	}()
	control, data := []byte{1, 2, 3}, []byte{4, 5, 6}
	if port.enqueue(control, true) != nil || port.enqueue(data, false) != nil {
		t.Fatal("queue setup failed")
	}
	data[0] = 99
	permission.set(connectivity.Permission{ExistingFlows: true})
	slab := make([]byte, 512)
	packets := make([]wgtun.ReadPacket, 1)
	n, err := (wireGuardPort{port}).Read(slab, packets)
	if err != nil || n != 1 || !bytes.Equal(slab[packets[0].Offset:packets[0].Offset+packets[0].Size], []byte{4, 5, 6}) {
		t.Fatal("offline control escaped or caller changed queued data", err)
	}
	if err := port.enqueue(control, true); err != ErrNoAuthority {
		t.Fatal("offline operation created a new probe")
	}
}

func TestVirtualPortBoundsBytesAndFramesAndCloseIsIsolated(t *testing.T) {
	allowed := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
	first, err := newPacketPort(t.Context(), 1420, allowed, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPacketPort(t.Context(), 1420, allowed, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	}()
	half := make([]byte, transport.MaxProbeBytes/2)
	for range 2 {
		if first.enqueue(half, true) != nil {
			t.Fatal("bounded queue cannot fill its byte budget")
		}
	}
	if err := first.enqueue([]byte{1}, true); err != ErrPacketIO {
		t.Fatal("queue exceeded byte budget")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.enqueue([]byte{1}, false); err != net.ErrClosed {
		t.Fatal("closed engine accepted data")
	}
	if err := second.enqueue([]byte{7}, false); err != nil {
		t.Fatal("closing one engine disabled another")
	}
	for i := 1; i < portQueueFrames; i++ {
		if second.enqueue([]byte{7}, false) != nil {
			t.Fatal("frame budget setup failed")
		}
	}
	if err := second.enqueue([]byte{7}, false); err != ErrPacketIO {
		t.Fatal("queue exceeded frame budget")
	}
}

func TestAWGReadCannotConsumeFirstPacketWithPreConfigurationOffset(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	allowed := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
	port, err := newPacketPort(parent, 1420, allowed, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := port.Close(); err != nil {
			t.Error(err)
		}
	}()
	configured := make(chan struct{})
	adapter := &amneziaPort{packetPort: port, configured: configured, expectedOffset: 40}
	if err := port.enqueue([]byte{1, 2, 3}, false); err != nil {
		t.Fatal(err)
	}
	close(configured)
	bufs, sizes := [][]byte{make([]byte, 128)}, []int{0}
	if n, err := adapter.Read(bufs, sizes, 16); err != nil || n != 0 {
		t.Fatal("stale S4 read consumed first packet", err)
	}
	if n, err := adapter.Read(bufs, sizes, 40); err != nil || n != 1 || !bytes.Equal(bufs[0][40:43], []byte{1, 2, 3}) {
		t.Fatal("first configured packet was lost", err)
	}
	if n, err := adapter.Read(bufs, sizes, 16); err != ErrConfiguration || n != 0 {
		t.Fatal("bad offset was retried indefinitely")
	}
}

func TestVirtualPortDropsStaleRouteReceiptBeforeEngineRead(t *testing.T) {
	allowed := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
	port, err := newPacketPort(t.Context(), 1420, allowed, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = port.Close() }()
	current := true
	if err := port.enqueueChecked([]byte{1}, false, func() bool { return current }); err != nil {
		t.Fatal(err)
	}
	current = false
	if err := port.enqueueChecked([]byte{2}, false, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	packet, err := port.next()
	if err != nil || !bytes.Equal(packet.data, []byte{2}) {
		t.Fatal("stale route receipt escaped into engine", err)
	}
	clear(packet.data)
	if port.queuedBytes != 0 {
		t.Fatal("discarded route packet retained byte reservation")
	}
}
