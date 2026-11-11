package dataplane

import (
	"context"
	"net"
	"os"
	"sync"
	"sync/atomic"

	awgtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
	"github.com/endless-net/client/api/transport"
	wgtun "github.com/tailscale/wireguard-go/tun"
)

const portQueueFrames = 16

type portPacket struct {
	data    []byte
	control bool
	guard   func() bool
}

// packetPort owns a bounded engine queue, never an OS adapter. Its sink receives
// plaintext only after the pinned engine's AEAD/replay/AllowedIPs checks. The
// native owner classifies probes and applies current source/ACL/lease policy.
type packetPort struct {
	mu          sync.Mutex
	queue       chan portPacket
	queuedBytes int
	mtu         int
	closed      bool
	done        chan struct{}
	cancel      context.CancelFunc
	once        sync.Once
	permission  permissionCheck
	sink        func([]byte) error
	wgEvents    chan wgtun.Event
	awgEvents   chan awgtun.Event
}

func newPacketPort(parent context.Context, mtu int, permission permissionCheck, sink func([]byte) error) (*packetPort, error) {
	if parent == nil || parent.Err() != nil || mtu < 576 || mtu > 65535 || permission == nil || sink == nil {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	p := &packetPort{queue: make(chan portPacket, portQueueFrames), mtu: mtu, permission: permission, sink: sink, done: make(chan struct{}), cancel: cancel,
		wgEvents: make(chan wgtun.Event), awgEvents: make(chan awgtun.Event)}
	context.AfterFunc(ctx, func() { _ = p.Close() })
	return p, nil
}

func (p *packetPort) allowed(control bool) bool {
	permission := p.permission()
	return permission.ExistingFlows && (!control || permission.NewFlows)
}

func (p *packetPort) enqueue(raw []byte, control bool) error {
	return p.enqueueChecked(raw, control, nil)
}

func (p *packetPort) enqueueChecked(raw []byte, control bool, guard func() bool) error {
	if len(raw) == 0 || len(raw) > 65535 || !control && len(raw) > p.mtu {
		return ErrPacketIO
	}
	if !p.allowed(control) || guard != nil && !guard() {
		return ErrNoAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return net.ErrClosed
	}
	if len(p.queue) == cap(p.queue) || len(raw) > transport.MaxProbeBytes-p.queuedBytes {
		return ErrPacketIO
	}
	packet := append([]byte(nil), raw...)
	p.queuedBytes += len(packet)
	p.queue <- portPacket{data: packet, control: control, guard: guard}
	return nil
}

func (p *packetPort) next() (portPacket, error) {
	for {
		select {
		case <-p.done:
			return portPacket{}, net.ErrClosed
		case packet := <-p.queue:
			p.mu.Lock()
			p.queuedBytes -= len(packet.data)
			closed := p.closed
			p.mu.Unlock()
			if closed {
				clear(packet.data)
				return portPacket{}, net.ErrClosed
			}
			if !p.allowed(packet.control) || packet.guard != nil && !packet.guard() {
				clear(packet.data)
				continue
			}
			return packet, nil
		}
	}
}

func (p *packetPort) Write(bufs [][]byte, offset int) (int, error) {
	if offset < 0 {
		return 0, ErrPacketIO
	}
	for i, raw := range bufs {
		if offset > len(raw) || len(raw)-offset > 65535 {
			return i, ErrPacketIO
		}
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return i, net.ErrClosed
		}
		if !p.allowed(false) {
			return i, ErrNoAuthority
		}
		if err := p.sink(raw[offset:]); err != nil {
			return i, err
		}
		if !p.allowed(false) {
			return i + 1, ErrNoAuthority
		}
	}
	return len(bufs), nil
}

func (p *packetPort) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		for {
			select {
			case packet := <-p.queue:
				p.queuedBytes -= len(packet.data)
				clear(packet.data)
			default:
				close(p.done)
				close(p.wgEvents)
				close(p.awgEvents)
				p.mu.Unlock()
				p.cancel()
				return
			}
		}
	})
	return nil
}

func (*packetPort) File() *os.File        { return nil }
func (p *packetPort) MTU() (int, error)   { return p.mtu, nil }
func (*packetPort) Name() (string, error) { return "endlessnet-virtual-peer", nil }
func (*packetPort) BatchSize() int        { return 1 }

type wireGuardPort struct{ *packetPort }

func (p wireGuardPort) Events() <-chan wgtun.Event { return p.wgEvents }
func (p wireGuardPort) Read(slab []byte, packets []wgtun.ReadPacket) (int, error) {
	if len(packets) < 1 || len(slab) < 2*wgtun.ReadPacketSpacing {
		return 0, ErrPacketIO
	}
	packet, err := p.next()
	if err != nil {
		return 0, err
	}
	defer clear(packet.data)
	if len(packet.data) > len(slab)-2*wgtun.ReadPacketSpacing {
		return 0, ErrPacketIO
	}
	copy(slab[wgtun.ReadPacketSpacing:], packet.data)
	packets[0] = wgtun.ReadPacket{Offset: wgtun.ReadPacketSpacing, Size: len(packet.data)}
	return 1, nil
}

type amneziaPort struct {
	*packetPort
	configured     <-chan struct{}
	expectedOffset int
	retried        atomic.Bool
}

func (p *amneziaPort) Events() <-chan awgtun.Event { return p.awgEvents }
func (p *amneziaPort) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	// NewDevice may start a Read using default S4 before IpcSet applies the
	// profile. Release that stale read without consuming a packet, so upstream
	// reloads its immutable transport padding before constructing ciphertext.
	if p.configured != nil {
		select {
		case <-p.done:
			return 0, net.ErrClosed
		case <-p.configured:
		}
		if offset != p.expectedOffset {
			if p.retried.CompareAndSwap(false, true) {
				return 0, nil
			}
			return 0, ErrConfiguration
		}
	}

	if offset < 0 || len(bufs) < 1 || len(sizes) < 1 || offset > len(bufs[0]) {
		return 0, ErrPacketIO
	}
	packet, err := p.next()
	if err != nil {
		return 0, err
	}
	defer clear(packet.data)
	if len(packet.data) > len(bufs[0])-offset {
		return 0, ErrPacketIO
	}
	copy(bufs[0][offset:], packet.data)
	sizes[0] = len(packet.data)
	return 1, nil
}

var _ wgtun.Device = wireGuardPort{}
var _ awgtun.Device = (*amneziaPort)(nil)
