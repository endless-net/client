package dataplane

import (
	"errors"
	"sync/atomic"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/tun"
)

var ErrNoAuthority = errors.New("dataplane authority unavailable")
var ErrPacketIO = errors.New("invalid dataplane packet I/O")

// permissionCheck is private: production Engine always binds it to the current
// intent's Lease. Tests can move the permission boundary without changing OS
// clocks or allocating a network adapter.
type permissionCheck func() connectivity.Permission

// leaseTUN rechecks after a blocking read and immediately before delivery to the
// OS. It does not track flows: directed stateful authorization and the ban on
// opening new flows offline must be enforced by the native policy boundary.
// ExistingFlows is only the outer, finite lifetime of that narrower permission.
//
// The wrapper intentionally exposes a single guarded queue; exposing the native
// MultiQueueDevice/WriteTo would let the engine bypass these checks.
type leaseTUN struct {
	tun.Device
	permissions permissionCheck
}

func (t *leaseTUN) Read(slab []byte, packets []tun.ReadPacket) (int, error) {
	for {
		n, err := t.Device.Read(slab, packets)
		if n < 0 || n > len(packets) {
			return 0, ErrPacketIO
		}
		kept := 0
		for i := 0; i < n; i++ {
			p := packets[i]
			if p.Offset < 0 || p.Size < 0 || p.Offset > len(slab) || p.Size > len(slab)-p.Offset {
				return 0, ErrPacketIO
			}
			if t.permissions().ExistingFlows {
				packets[kept] = p
				kept++
			}
		}
		if kept != 0 || err != nil || n == 0 {
			return kept, err
		}
		// Dropped data is never queued for a future grant. Continue with the
		// underlying blocking reader; Close is responsible for unblocking it.
	}
}

func (t *leaseTUN) Write(bufs [][]byte, offset int) (int, error) {
	if offset < 0 {
		return 0, ErrPacketIO
	}
	for i, packet := range bufs {
		if offset > len(packet) {
			return i, ErrPacketIO
		}
		if !t.permissions().ExistingFlows {
			continue // consumed and dropped, never retried under a later grant
		}
		n, err := t.Device.Write(bufs[i:i+1], offset)
		if n < 0 || n > 1 {
			return i, ErrPacketIO
		}
		if err != nil {
			return i + n, err
		}
		if n != 1 {
			return i, ErrPacketIO
		}
	}
	return len(bufs), nil
}

// leaseBind closes the post-encryption queue gap: a packet read while authority
// was valid may reach Send after suspend/expiry/revoke. Checking only TUN.Read or
// a background timer would allow that queued encrypted packet to escape.
type leaseBind struct {
	conn.Bind
	permissions permissionCheck
	listenPort  uint16
	port        atomic.Uint32
}

func (b *leaseBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	if b.listenPort == 0 || port != b.listenPort {
		return nil, 0, ErrConfiguration
	}
	fns, actual, err := b.Bind.Open(port)
	if err != nil {
		b.port.Store(0)
		_ = b.Bind.Close()
		return nil, 0, ErrEngine
	}
	valid := actual == port && len(fns) > 0
	for _, receive := range fns {
		valid = valid && receive != nil
	}
	if !valid {
		b.port.Store(0)
		_ = b.Bind.Close()
		return nil, 0, ErrEngine
	}
	b.port.Store(uint32(actual))
	return fns, actual, nil
}

func (b *leaseBind) Close() error {
	b.port.Store(0)
	return b.Bind.Close()
}

func (b *leaseBind) Send(bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	if offset < 0 {
		return ErrPacketIO
	}
	for i, packet := range bufs {
		if offset > len(packet) {
			return ErrPacketIO
		}
		if !b.permissions().ExistingFlows {
			return ErrNoAuthority
		}
		if err := b.Bind.Send(bufs[i:i+1], endpoint, offset); err != nil {
			return err
		}
	}
	return nil
}
