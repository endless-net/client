package dataplane

import (
	"net/netip"
	"testing"

	"github.com/endless-net/client/internal/connectivity"
)

func TestLinuxManagedRouteInventoryMatchesExactPrefixAndSource(t *testing.T) {
	host := netip.MustParseAddr("100.64.0.2")
	source := netip.MustParseAddr("100.64.0.1")
	path := connectivity.ResourcePath{Prefix: netip.MustParsePrefix("10.72.0.0/24")}
	routes := linuxRouteDestinations([]netip.Addr{host}, []connectivity.ResourcePath{path, path})
	if len(routes) != 2 {
		t.Fatal("host or shared subnet was duplicated")
	}
	const hostRow = `{"dst":"100.64.0.2","prefsrc":"100.64.0.1"}`
	for _, tc := range []struct {
		other string
		valid bool
	}{
		{`{"dst":"10.72.0.0/24","prefsrc":"100.64.0.1"}`, true},
		{`{"dst":"10.72.0.0/25","prefsrc":"100.64.0.1"}`, false},
		{`{"dst":"10.72.0.0/24","prefsrc":"100.64.0.3"}`, false},
		{`{"dst":"default","prefsrc":"100.64.0.1"}`, false}, {hostRow, false},
	} {
		if err := verifyLinuxRouteInventory([]byte("["+hostRow+","+tc.other+"]"), source, routes); (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
	}
	if err := verifyLinuxRouteInventory([]byte("["+hostRow+"]"), source, linuxRouteDestinations([]netip.Addr{host}, nil)); err != nil {
		t.Fatal("subnet withdrawal removed independent host", err)
	}
}

func TestLinuxResourceRouteConflictsBeforeMutation(t *testing.T) {
	src := netip.MustParseAddr("100.64.0.1")
	owned := []netip.Prefix{netip.MustParsePrefix("10.72.0.0/24")}
	for _, tc := range []struct {
		raw     string
		allowed bool
	}{
		{`[]`, false},
		{`[{"dst":"10.72.0.0/24","dev":"en0","prefsrc":"100.64.0.1","protocol":99}]`, true},
		{`[{"dst":"10.72.0.0/24","dev":"en0","prefsrc":"100.64.0.1","protocol":"99","table":254}]`, true},
		{`[{"dst":"10.72.0.0/24","dev":"eth0","prefsrc":"100.64.0.1","protocol":99}]`, false},
		{`[{"dst":"10.72.0.0/25","dev":"en0","prefsrc":"100.64.0.1","protocol":99}]`, false},
		{`[{"dst":"10.72.0.0/24","dev":"en0","prefsrc":"100.64.0.1","protocol":99,"table":100}]`, false},
		{`[{"dst":"10.72.0.0/24","dev":"en0","prefsrc":"100.64.0.1","protocol":3}]`, false},
		{`null`, false},
	} {
		if err := checkLinuxRouteSpace([]byte(tc.raw), "en0", src, owned, owned, nil); (err == nil) != tc.allowed {
			t.Fatal(tc.raw, err)
		}
	}
}

func TestLinuxRouteReadbackDetectsChangedNextHopAndKeepsPhysicalDefault(t *testing.T) {
	source := netip.MustParseAddr("100.64.0.1")
	subnet := netip.MustParsePrefix("10.72.0.0/24")
	owned := []netip.Prefix{subnet}
	const attrs = `"dst":"10.72.0.0/24","dev":"en0","protocol":99,"prefsrc":"100.64.0.1"`
	const other = `{"dst":"default","dev":"eth0","gateway":"192.0.2.1","metric":10},{"dst":"192.0.2.0/24","dev":"eth0","protocol":2}`
	for _, extra := range []string{`"gateway":"192.0.2.1"`, `"nhid":3`, `"nexthops":[{"dev":"eth0"}]`, `"metric":50`, `"scope":"global"`, `"type":"blackhole"`, `"flags":["linkdown"]`, `"tos":"0x10"`} {
		row := "{" + attrs + "," + extra + "}"
		if err := checkLinuxRouteSpace([]byte("["+other+","+row+"]"), "en0", source, owned, owned, nil); err != ErrIPConflict {
			t.Fatal("competing route", extra, err)
		}
		if err := verifyLinuxRouteInventory([]byte("["+row+"]"), source, owned); err != ErrIPOwnership {
			t.Fatal("modified route receipt", extra, err)
		}
	}
	row := "{" + attrs + `,"scope":253,"type":1,"metric":0,"flags":[]}`
	snapshot := "[" + other + "," + row
	if err := checkLinuxRouteSpace([]byte(snapshot+"]"), "en0", source, owned, owned, nil); err != nil {
		t.Fatal("physical default or independent LAN rejected", err)
	}
	if err := checkLinuxRouteSpace([]byte(snapshot+`,{"dst":"10.72.0.73","dev":"eth0","protocol":4,"table":101}]`), "en0", source, owned, owned, nil); err != ErrIPConflict {
		t.Fatal("more specific foreign route went undetected", err)
	}
	if err := checkLinuxRouteSpace([]byte("["+other+"]"), "en0", source, owned, owned, nil); err != ErrIPOwnership {
		t.Fatal("missing installed route was confirmed", err)
	}
	if err := checkLinuxRouteSpace([]byte("["+other+"]"), "en0", source, nil, owned, nil); err != nil {
		t.Fatal("new route rejected", err)
	}
}
