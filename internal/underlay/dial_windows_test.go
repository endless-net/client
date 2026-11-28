package underlay

import (
	"errors"
	"net/netip"
	"testing"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestWindowsOuterRouteUsesPhysicalPrefixAndCombinedMetric(t *testing.T) {
	ip := netip.MustParseAddr("8.8.8.8")
	row := func(prefix string, index, metric uint32) winipcfg.MibIPforwardRow2 {
		var r winipcfg.MibIPforwardRow2
		r.Init()
		r.ValidLifetime = ^uint32(0)
		r.Loopback = false
		if err := r.DestinationPrefix.SetPrefix(netip.MustParsePrefix(prefix)); err != nil {
			t.Fatal(err)
		}
		r.InterfaceLUID, r.InterfaceIndex, r.Metric = winipcfg.LUID(index), index, metric
		return r
	}
	rows := []winipcfg.MibIPforwardRow2{row("0.0.0.0/0", 1, 10), row("0.0.0.0/0", 2, 9), row("8.8.8.8/32", 3, 0), row("8.8.8.0/24", 4, 70)}
	iface := func(l winipcfg.LUID) (*winipcfg.MibIPInterfaceRow, error) {
		if l == 3 {
			return nil, nil
		}
		i := &winipcfg.MibIPInterfaceRow{InterfaceLUID: l, InterfaceIndex: uint32(l), Connected: true}
		if l == 2 {
			i.Metric = ^uint32(0)
		}
		return i, nil
	}
	if i, err := bestOuterRoute(ip, rows, iface); err != nil || i != 4 {
		t.Fatal(i, err)
	}
	if i, err := bestOuterRoute(ip, rows[:3], iface); err != nil || i != 1 {
		t.Fatal(i, err)
	}
	if _, err := bestOuterRoute(ip, rows[2:3], iface); !errors.Is(err, errNoInterface) {
		t.Fatal(err)
	}
	id := windows.GUID{Data1: 1234}
	one := ExcludeTunnel(1234, id)
	two := ExcludeTunnel(1234, id)
	one()
	if _, ok := tunnels.Load(winipcfg.LUID(1234)); !ok {
		t.Fatal("previous session released replacement adapter")
	}
	two()
	if _, ok := tunnels.Load(winipcfg.LUID(1234)); ok {
		t.Fatal("adapter exclusion survived cleanup")
	}
}

func TestWindowsReusedTunnelLUID(t *testing.T) {
	const luid = 1235
	guid := windows.GUID{Data1: 1}
	defer ExcludeTunnel(luid, guid)()
	lookup := func(winipcfg.LUID) (*windows.GUID, error) { return &guid, nil }
	if ok, err := excludedTunnel(luid, lookup); err != nil || !ok {
		t.Fatal(ok, err)
	}
	unknown := errors.New("interface disappeared")
	if _, err := excludedTunnel(luid, func(winipcfg.LUID) (*windows.GUID, error) { return nil, unknown }); !errors.Is(err, unknown) {
		t.Fatal(err)
	}
	guid.Data1++
	if ok, err := excludedTunnel(luid, lookup); err != nil || ok {
		t.Fatal(ok, err)
	}
	if _, ok := tunnels.Load(winipcfg.LUID(luid)); ok {
		t.Fatal("retained stale adapter identity")
	}
}
