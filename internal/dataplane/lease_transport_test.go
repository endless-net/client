package dataplane

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/tun"
)

type packetPermission struct {
	mu sync.Mutex
	p  connectivity.Permission
}

func (p *packetPermission) get() connectivity.Permission {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.p
}
func (p *packetPermission) set(v connectivity.Permission) { p.mu.Lock(); p.p = v; p.mu.Unlock() }

type packetTUN struct {
	*inertTUN
	read       func([]byte, []tun.ReadPacket) (int, error)
	written    [][]byte
	afterWrite func()
}

func (t *packetTUN) Read(slab []byte, packets []tun.ReadPacket) (int, error) {
	return t.read(slab, packets)
}
func (t *packetTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		t.written = append(t.written, bytes.Clone(b[offset:]))
	}
	if t.afterWrite != nil {
		t.afterWrite()
	}
	return len(bufs), nil
}

func TestLeaseTUNRechecksAfterBlockedReadWithoutReplayingDroppedData(t *testing.T) {
	for _, terminal := range []connectivity.Permission{{}, {Revoked: true}, {ClockUnsafe: true}} {
		t.Run(terminalName(terminal), func(t *testing.T) {
			permission := &packetPermission{p: connectivity.Permission{ExistingFlows: true, NewFlows: true}}
			started, resume := make(chan struct{}), make(chan struct{})
			reads := 0
			native := &packetTUN{inertTUN: newInertTUN()}
			native.read = func(slab []byte, packets []tun.ReadPacket) (int, error) {
				reads++
				if reads == 1 {
					close(started)
					<-resume
					copy(slab[64:], []byte("old packet"))
					packets[0] = tun.ReadPacket{Offset: 64, Size: len("old packet")}
					return 1, nil
				}
				return 0, io.EOF
			}
			guard := &leaseTUN{Device: native, permissions: permission.get}
			done := make(chan struct{})
			var n int
			var err error
			go func() { defer close(done); n, err = guard.Read(make([]byte, 256), make([]tun.ReadPacket, 1)) }()
			<-started
			permission.set(terminal) // deadline/revoke while the native read sleeps
			close(resume)
			<-done
			if n != 0 || !errors.Is(err, io.EOF) || reads != 2 {
				t.Fatalf("expired read escaped: n%d err%v reads%d", n, err, reads)
			}
			permission.set(connectivity.Permission{ExistingFlows: true, NewFlows: true})
			if n, err = guard.Read(make([]byte, 256), make([]tun.ReadPacket, 1)); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatal("a dropped packet was buffered until a fresh grant")
			}
		})
	}
}

func terminalName(p connectivity.Permission) string {
	if p.Revoked {
		return "revoked"
	}
	if p.ClockUnsafe {
		return "clock unsafe"
	}
	return "expired or cancelled"
}

func TestLeaseTUNChecksEachDecryptedPacketBeforeOSDelivery(t *testing.T) {
	permission := &packetPermission{p: connectivity.Permission{ExistingFlows: true}}
	native := &packetTUN{inertTUN: newInertTUN(), afterWrite: func() { permission.set(connectivity.Permission{}) }}
	guard := &leaseTUN{Device: native, permissions: permission.get}
	bufs := [][]byte{{0, 0, 11}, {0, 0, 22}, {0, 0, 33}}
	if n, err := guard.Write(bufs, 2); n != 3 || err != nil {
		t.Fatal(n, err)
	}
	if len(native.written) != 1 || !bytes.Equal(native.written[0], []byte{11}) {
		t.Fatal("post-expiry decrypted data reached OS")
	}
	permission.set(connectivity.Permission{ExistingFlows: true, NewFlows: true})
	if len(native.written) != 1 {
		t.Fatal("dropped data was retried")
	}
}

type packetBind struct {
	*inertBind
	sent      [][]byte
	afterSend func()
}

func (b *packetBind) Send(bufs [][]byte, _ conn.Endpoint, offset int) error {
	for _, p := range bufs {
		b.sent = append(b.sent, bytes.Clone(p[offset:]))
	}
	if b.afterSend != nil {
		b.afterSend()
	}
	return nil
}

func TestLeaseBindDropsPacketsQueuedBeforeExpiry(t *testing.T) {
	permission := &packetPermission{p: connectivity.Permission{ExistingFlows: true, NewFlows: true}}
	native := &packetBind{inertBind: &inertBind{}}
	guard := &leaseBind{Bind: native, permissions: permission.get}
	// Encryption completed under the previous authority; Send happens later.
	ciphertext := [][]byte{{0, 17, 18}, {0, 27, 28}}
	permission.set(connectivity.Permission{})
	if err := guard.Send(ciphertext, nil, 1); !errors.Is(err, ErrNoAuthority) || len(native.sent) != 0 {
		t.Fatal("queued ciphertext escaped", err)
	}
	permission.set(connectivity.Permission{ExistingFlows: true})
	native.afterSend = func() { permission.set(connectivity.Permission{Revoked: true}) }
	if err := guard.Send(ciphertext, nil, 1); !errors.Is(err, ErrNoAuthority) || len(native.sent) != 1 || !bytes.Equal(native.sent[0], []byte{17, 18}) {
		t.Fatal("a batch crossed the revocation boundary", err)
	}
}

type multiQueueTUN struct{ *packetTUN }

func (t *multiQueueTUN) Queues() []tun.Queue { return []tun.Queue{t.packetTUN} }
func (t *multiQueueTUN) WriteTo(_ int, bufs [][]byte, offset int) (int, error) {
	return t.Write(bufs, offset)
}

type multiSocketBind struct{ *packetBind }

func (b *multiSocketBind) SendTo(_ int, bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	return b.Send(bufs, endpoint, offset)
}

func TestLeaseWrappersDoNotExposeQueueBypasses(t *testing.T) {
	native := &multiQueueTUN{packetTUN: &packetTUN{inertTUN: newInertTUN()}}
	tunnel := &leaseTUN{Device: native, permissions: func() connectivity.Permission { return connectivity.Permission{} }}
	if _, ok := any(tunnel).(tun.MultiQueueDevice); ok {
		t.Fatal("unguarded multi-queue API exposed")
	}
	if queues := tun.QueuesOf(tunnel); len(queues) != 1 || queues[0] != tunnel {
		t.Fatal("read bypass")
	}
	nativeBind := &multiSocketBind{packetBind: &packetBind{inertBind: &inertBind{}}}
	bind := &leaseBind{Bind: nativeBind, permissions: func() connectivity.Permission { return connectivity.Permission{} }}
	if _, ok := any(bind).(conn.MultiSocketBind); ok {
		t.Fatal("unguarded SendTo exposed")
	}
	if err := conn.SendToOf(bind)(0, [][]byte{{1}}, nil, 0); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("send bypass", err)
	}
	if len(nativeBind.sent) != 0 {
		t.Fatal("specialized SendTo bypassed guard")
	}
	if n, err := tun.WriteToOf(tunnel)(0, [][]byte{{1}}, 0); n != 1 || err != nil || len(native.written) != 0 {
		t.Fatal("specialized WriteTo bypassed guard", n, err)
	}
	guard := &leaseTUN{Device: native, permissions: func() connectivity.Permission { return connectivity.Permission{ExistingFlows: true} }}
	if n, err := tun.WriteToOf(guard)(0, [][]byte{{1}}, 0); n != 1 || err != nil || len(native.written) != 1 {
		t.Fatal("write dispatch missed guarded wrapper", n, err)
	}
}

func TestLeaseReadKeepsOriginalPacketOffsetsAndTerminalError(t *testing.T) {
	checks := 0
	native := &packetTUN{inertTUN: newInertTUN(), read: func(slab []byte, packets []tun.ReadPacket) (int, error) {
		copy(slab[64:], []byte{11, 12})
		copy(slab[130:], []byte{21, 22})
		packets[0], packets[1] = tun.ReadPacket{Offset: 64, Size: 2}, tun.ReadPacket{Offset: 130, Size: 2}
		return 2, io.EOF
	}}
	guard := &leaseTUN{Device: native, permissions: func() connectivity.Permission {
		checks++
		return connectivity.Permission{ExistingFlows: checks == 2}
	}}
	slab, packets := make([]byte, 256), make([]tun.ReadPacket, 2)
	n, err := guard.Read(slab, packets)
	if n != 1 || !errors.Is(err, io.EOF) || packets[0].Offset != 130 || !bytes.Equal(slab[packets[0].Offset:packets[0].Offset+packets[0].Size], []byte{21, 22}) {
		t.Fatal("filtered batch violated upstream reader contract", n, err)
	}
}

func TestLeaseTransportRejectsMalformedBatchBounds(t *testing.T) {
	permission := func() connectivity.Permission { return connectivity.Permission{ExistingFlows: true} }
	for _, packet := range []tun.ReadPacket{{Offset: -1, Size: 1}, {Offset: 0, Size: -1}, {Offset: 255, Size: 2}, {Offset: 300, Size: 1}} {
		native := &packetTUN{inertTUN: newInertTUN(), read: func(_ []byte, packets []tun.ReadPacket) (int, error) { packets[0] = packet; return 1, nil }}
		if n, err := (&leaseTUN{Device: native, permissions: permission}).Read(make([]byte, 256), make([]tun.ReadPacket, 1)); n != 0 || !errors.Is(err, ErrPacketIO) {
			t.Fatal("malformed packet accepted", n, err)
		}
	}
	bind := &leaseBind{Bind: &packetBind{inertBind: &inertBind{}}, permissions: permission}
	if err := bind.Send([][]byte{{1}}, nil, 2); !errors.Is(err, ErrPacketIO) {
		t.Fatal(err)
	}
	if n, err := (&leaseTUN{Device: newInertTUN(), permissions: permission}).Write([][]byte{{1}}, -1); n != 0 || !errors.Is(err, ErrPacketIO) {
		t.Fatal(n, err)
	}
}

func TestEngineRequiresLeaseAndStartsWithoutTrafficAuthority(t *testing.T) {
	native := &packetTUN{inertTUN: newInertTUN(), read: func([]byte, []tun.ReadPacket) (int, error) { return 0, io.EOF }}
	if _, err := NewEngine(native, &inertBind{}, 4, 51820, nil); !errors.Is(err, ErrConfiguration) {
		t.Fatal("missing lease accepted", err)
	}
	select {
	case <-native.done:
		t.Fatal("invalid constructor consumed caller resource")
	default:
	}
	lease := engineLease(t)
	bind := &packetBind{inertBind: &inertBind{}}
	if err := (&leaseBind{Bind: bind, permissions: lease.Permissions}).Send([][]byte{{1}}, nil, 0); !errors.Is(err, ErrNoAuthority) || len(bind.sent) != 0 {
		t.Fatal("restart lease restored authority", err)
	}
}
