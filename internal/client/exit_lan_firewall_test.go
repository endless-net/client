package client

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitLANFirewallRequiresFinalExactBinding(t *testing.T) {
	p := netip.MustParsePrefix("192.0.2.0/24")
	lan := &exitLANFirewallState{plan: &exitLANPlan{bindings: []exitLANBinding{{linkIndex: 2, linkName: "eth0", connected: p, sources: []netip.Addr{netip.MustParseAddr("192.0.2.2")}, destinations: []netip.Prefix{netip.MustParsePrefix("192.0.2.128/25")}}}}, routing: &exitLANRoutingOwnership{Family: api.ExitFamilyIPv4Only, Table: 1073793644, Mark: 2147535468, Routes: []exitLANOwnedRoute{{"192.0.2.128/25", "eth0"}}}}
	batch, output, marking, err := lan.rules("owned", 51820)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"add rule inet owned lanroute meta mark != 51820 ip daddr 192.0.2.128/25 meta mark set 2147535468\n",
		"add rule inet owned output meta mark 2147535468 meta oif 2 ip saddr 192.0.2.2 ip daddr 192.0.2.128/25 accept\n",
		"add rule inet owned lanfinal meta mark 2147535468 meta oif 2 ip saddr 192.0.2.2 ip daddr 192.0.2.128/25 accept\n",
		"add rule inet owned lanfinal meta mark 2147535468 drop\n",
	} {
		if !strings.Contains(batch, line) {
			t.Fatal("missing final/source/destination binding", line)
		}
	}
	// Independent libnftables fixture: implicit inet prerequisite is allowed,
	// but deleting or changing any actual binding or verdict is never evidence.
	const fixture = `[[{"match":{"op":"==","left":{"meta":{"key":"mark"}},"right":2147535468}},{"match":{"op":"==","left":{"meta":{"key":"oif"}},"right":2}},{"match":{"op":"==","left":{"meta":{"key":"nfproto"}},"right":2}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"192.0.2.2"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"prefix":{"addr":"192.0.2.128","len":25}}}},{"accept":null}]]`
	decode := func(raw string) []any {
		var rows []any
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if !exitGuardLANRulesEqual(decode(fixture), output) || len(marking) != 2 || len(lan.finalRules(output)) != 2 {
		t.Fatal("exact rule readback failed")
	}
	for _, changed := range []string{
		strings.Replace(fixture, `"right":2}`, `"right":3}`, 1),
		strings.ReplaceAll(fixture, `192.0.2.2`, `192.0.2.3`),
		strings.ReplaceAll(fixture, `"len":25`, `"len":24`),
		strings.ReplaceAll(fixture, `"op":"=="`, `"op":"!="`),
		strings.ReplaceAll(fixture, `"accept":null`, `"drop":null`),
	} {
		if exitGuardLANRulesEqual(decode(changed), output) {
			t.Fatal("changed packet binding accepted")
		}
	}
	final := lan.finalRules(output)
	if exitGuardLANRulesEqual(final[:1], final) {
		t.Fatal("missing terminal drop accepted")
	}
}
