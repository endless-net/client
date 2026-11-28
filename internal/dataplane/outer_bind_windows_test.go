package dataplane

import (
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/tailscale/wireguard-go/conn"
)

type outerBindFixture struct {
	inertBind
	active uint32
	sent   int
	fail   bool
}

func (f *outerBindFixture) BindSocketToInterface4(index uint32, blackhole bool) error {
	if f.fail || blackhole {
		return ErrIPConflict
	}
	f.active = index
	return nil
}

func (f *outerBindFixture) Send(_ [][]byte, e conn.Endpoint, _ int) error {
	if f.active != uint32(e.DstIP().As4()[3]) {
		return ErrIPConflict
	}
	f.sent++
	return nil
}

func TestWindowsWireGuardBindsEachOuterPeerBeforeSend(t *testing.T) {
	fixture := new(outerBindFixture)
	b := &windowsOuterBind{Bind: fixture, index: func(a netip.Addr) (uint32, error) { return uint32(a.As4()[3]), nil }}
	if err := b.pin(); err != nil {
		t.Fatal(err)
	}
	var done sync.WaitGroup
	for _, ip := range []string{"192.0.2.7:1000", "192.0.2.9:1000"} {
		e, err := conn.NewStdNetBind().ParseEndpoint(ip)
		if err != nil {
			t.Fatal(err)
		}
		done.Go(func() {
			for range 20 {
				if err := b.Send(nil, e, 0); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	done.Wait()
	fixture.fail = true
	e, _ := conn.NewStdNetBind().ParseEndpoint("192.0.2.8:1000")
	if err := b.Send(nil, e, 0); !errors.Is(err, ErrIPConflict) || fixture.sent != 40 {
		t.Fatal(err, fixture.sent)
	}
}
