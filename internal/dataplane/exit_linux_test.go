package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
)

func TestLinuxExitRuleWithdrawalRecoversLostReply(t *testing.T) {
	receipt := state.LinuxNetworkReceipt{Owner: "487fb261-c50e-4fad-8e3a-47622509959b", ExitRule: true}
	table, err := linuxExitTable(receipt.Owner)
	if err != nil {
		t.Fatal(err)
	}
	local := `{"priority":0,"src":"all","table":"255","protocol":"2"}`
	own := fmt.Sprintf(`{"priority":32000,"src":"all","iif":"lo","fwmark":"0","table":"%s","protocol":"99"}`, table)
	raw := func(rows ...string) []byte { return []byte("[" + strings.Join(rows, ",") + "]") }
	if found, err := linuxExitRulePresent(raw(local, own), table, true); err != nil || !found {
		t.Fatal(found, err)
	}
	for _, invalid := range [][]byte{
		raw(local, own, own), raw(local, strings.Replace(own, "lo", "eth0", 1)),
		raw(local, strings.Replace(own, `"fwmark":"0"`, `"fwmark":"0","fwmask":"0x1"`, 1)),
		raw(local, strings.Replace(own, table, "100", 1)), raw(own),
		raw(local, `{"priority":1000,"src":"all","table":"100","protocol":"2"}`),
	} {
		if _, err := linuxExitRulePresent(invalid, table, true); !errors.Is(err, ErrIPConflict) {
			t.Fatal(string(invalid), err)
		}
	}
	other := strings.Replace(own, table, "100", 1)
	rows := raw(local, own, other)
	deletes := 0
	run := func(_ context.Context, args ...string) ([]byte, error) {
		if slices.Contains(args, "show") {
			return rows, nil
		}
		if !slices.Equal(args, []string{"-4", "rule", "del", "pref", "32000", "iif", "lo", "fwmark", "0/0xffffffff", "lookup", table, "protocol", "99"}) {
			t.Fatal(args)
		}
		deletes++
		rows = raw(local, other)
		return nil, context.DeadlineExceeded
	}
	if err := removeLinuxExitRule(t.Context(), run, receipt); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := removeLinuxExitRule(t.Context(), run, receipt); err != nil || deletes != 1 {
		t.Fatal(err, deletes)
	}
}

func TestLinuxExitTableKeepsUnderlayAndSpecificRoutes(t *testing.T) {
	overlay := netip.MustParsePrefix("23.45.0.0/16")
	ps, err := exitPublicPrefixes(overlay)
	if err != nil {
		t.Fatal(err)
	}
	for ip, expected := range map[string]bool{"8.8.8.8": true, "1.1.1.1": true, "23.45.0.1": false, "10.30.0.1": false, "127.0.0.1": false, "224.0.0.1": false} {
		if slices.ContainsFunc(ps, func(p netip.Prefix) bool { return p.Contains(netip.MustParseAddr(ip)) }) != expected {
			t.Fatal(ip)
		}
	}
	lan := netip.MustParsePrefix("10.30.0.0/24")
	paths := []connectivity.ResourcePath{{Prefix: lan}, {Prefix: netip.MustParsePrefix("0.0.0.0/0")}}
	if !slices.Equal(linuxRouteDestinations(nil, paths), linuxRouteDestinations(nil, paths[:1])) {
		t.Fatal("exit affected main table")
	}
	exit := linuxExitRoutes{table: "141414", prefixes: []netip.Prefix{netip.MustParsePrefix("8.0.0.0/8")}}
	source := netip.MustParseAddr("23.45.0.1")
	const physical = `{"dst":"default","gateway":"10.0.0.1","dev":"eth0"}`
	const own = `{"dst":"8.0.0.0/8","dev":"en017","prefsrc":"23.45.0.1","protocol":99,"table":141414}`
	check := func(installed bool, want error, rows ...string) {
		t.Helper()
		if err := exit.checkRows([]byte("["+strings.Join(rows, ",")+"]"), "en017", source, installed); !errors.Is(err, want) {
			t.Fatal(rows, err, want)
		}
	}
	check(false, nil, physical)
	check(true, nil, physical, own)
	check(true, ErrIPOwnership, physical)
	check(false, ErrIPConflict, physical, own)
	check(true, ErrIPConflict, physical, own, own)
	check(true, ErrIPConflict, physical, strings.Replace(own, "en017", "eth1", 1))
	check(false, ErrIPConflict, physical, `{"dst":"8.8.8.8","dev":"vpn1"}`)
}
