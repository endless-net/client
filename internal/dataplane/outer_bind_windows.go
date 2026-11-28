package dataplane

import (
	"net/netip"
	"sync"

	"github.com/tailscale/wireguard-go/conn"
)

type windowsInterfaceBind interface {
	conn.Bind
	BindSocketToInterface4(uint32, bool) error
}

// A primary WireGuard socket is shared by peers on potentially different
// interfaces. Keep interface selection and the corresponding send together.
type windowsOuterBind struct {
	conn.Bind
	mu     sync.Mutex
	pinned windowsInterfaceBind
	index  func(netip.Addr) (uint32, error)
}

func (b *windowsOuterBind) pin() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	outer, ok := b.Bind.(windowsInterfaceBind)
	if !ok {
		return ErrConfiguration
	}
	b.pinned = outer
	return nil
}

func (b *windowsOuterBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Bind.Open(port)
}

func (b *windowsOuterBind) Send(bufs [][]byte, peer conn.Endpoint, offset int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pinned == nil {
		return b.Bind.Send(bufs, peer, offset)
	}
	if peer == nil {
		return ErrNoAuthority
	}
	index, err := b.index(peer.DstIP())
	if err != nil || index == 0 {
		return ErrIPConflict
	}
	if err := b.pinned.BindSocketToInterface4(index, false); err != nil {
		return ErrIPConflict
	}
	return b.Bind.Send(bufs, peer, offset)
}
