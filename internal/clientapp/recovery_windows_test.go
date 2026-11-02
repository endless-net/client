//go:build windows

package clientapp

import (
	"net/netip"
	"reflect"
	"testing"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestRecoveryUnderlayIgnoresOwnSplitRoutesAndDetectsInterfaceChange(t *testing.T) {
	route := func(prefix string, index uint32) winipcfg.MibIPforwardRow2 {
		var r winipcfg.MibIPforwardRow2
		r.InterfaceIndex = index
		if err := r.DestinationPrefix.SetPrefix(netip.MustParsePrefix(prefix)); err != nil {
			t.Fatal(err)
		}
		if err := r.NextHop.SetAddr(netip.MustParseAddr("192.0.2.1")); err != nil {
			t.Fatal(err)
		}
		return r
	}
	base := []winipcfg.MibIPforwardRow2{route("0.0.0.0/0", 7)}
	a, ids := underlayRoutes(base)
	b, _ := underlayRoutes(append(base, route("100.64.0.9/32", 99)))
	if !reflect.DeepEqual(a, b) || len(ids) != 1 || !ids[7] {
		t.Fatal("own overlay created underlay event")
	}
	c, _ := underlayRoutes([]winipcfg.MibIPforwardRow2{route("0.0.0.0/0", 8)})
	if reflect.DeepEqual(a, c) {
		t.Fatal("new physical interface ignored")
	}
}

func TestRecoveryAdvertisesChangedLocalAddressWithoutInventingNATMapping(t *testing.T) {
	configured := netip.MustParseAddrPort("192.0.2.7:51820")
	changed := netip.MustParseAddr("198.51.100.8")
	got, err := selectUnderlayEndpoint(configured, []netip.Addr{changed})
	if err != nil || got != netip.AddrPortFrom(changed, configured.Port()) {
		t.Fatal("old endpoint retained across network change", got, err)
	}
	if _, err := selectUnderlayEndpoint(configured, nil); err == nil {
		t.Fatal("no-path selected stale endpoint")
	}
	if _, err := selectUnderlayEndpoint(configured, []netip.Addr{changed, netip.MustParseAddr("203.0.113.4")}); err == nil {
		t.Fatal("ambiguous address silently selected")
	}
	got, err = selectUnderlayEndpoint(configured, []netip.Addr{configured.Addr(), changed})
	if err != nil || got != configured {
		t.Fatal("explicit available address changed")
	}
}
