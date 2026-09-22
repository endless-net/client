package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitGuardEmptyTablesFixture = `{"nftables":[{"metainfo":{"version":"1.1.5","release_name":"test fixture","json_schema_version":1}}]}`

func exitGuardFamilyReadbackFixture(table, device string, mark uint32, open bool, family api.ExitFamilyMode) []byte {
	if !open || family == api.ExitFamilyDualStack {
		return exitGuardReadbackFixture(table, device, mark, open)
	}
	selected, ordinary := 2, 10
	if family == api.ExitFamilyIPv6Only {
		selected, ordinary = 10, 2
	}
	base := strings.TrimSuffix(string(exitGuardReadbackFixture(table, device, mark, false)), "]}")
	for _, chain := range []string{"output", "forward"} {
		base += fmt.Sprintf(`,{"rule":{"family":"inet","table":%q,"chain":%q,"expr":[{"match":{"op":"==","left":{"meta":{"key":"nfproto"}},"right":%d}},{"accept":null}]}}`, table, chain, ordinary)
	}
	base += fmt.Sprintf(`,{"rule":{"family":"inet","table":%q,"chain":"output","expr":[{"match":{"op":"==","left":{"meta":{"key":"nfproto"}},"right":%d}},{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":%q}},{"accept":null}]}}]}`, table, selected, device)
	return []byte(base)
}

func TestExitGuardReadbackRequiresExactSelectedFamilyInBothChains(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only} {
		good := string(exitGuardFamilyReadbackFixture("owned", "exit0", 51820, true, family))
		if !exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, true, family) {
			t.Fatal("family fixture rejected")
		}
		other := api.ExitFamilyIPv4Only
		if family == other {
			other = api.ExitFamilyIPv6Only
		}
		if exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, true, other) || exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, false, "") {
			t.Fatal("incorrect family/state accepted")
		}
		for _, bad := range []string{
			strings.Replace(good, `"chain":"forward"`, `"chain":"output"`, 1),
			strings.ReplaceAll(good, `"key":"nfproto"`, `"key":"l4proto"`),
			strings.ReplaceAll(good, `"right":10`, `"right":2`),
			strings.Replace(good, `"name":"forward"`, `"name":"other"`, 1),
		} {
			if exitGuardRulesObserved([]byte(bad), "owned", "exit0", 51820, true, family) {
				t.Fatal("widened family rule accepted")
			}
		}
	}
}

// A numeric libnftables-json fixture, independent of the production matcher.
// Objects follow nft listing order: each chain precedes its own rules.
func exitGuardReadbackFixture(table, device string, mark uint32, open bool) []byte {
	extra := ""
	if open {
		extra = fmt.Sprintf(`,{"rule":{"family":"inet","table":%q,"chain":"output","handle":8,"expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":%q}},{"accept":null}]}}`, table, device)
	}
	dhcp := ""
	for _, ports := range []struct{ family, source, destination, sport, dport string }{{"2", "", "", "68", "67"}, {"10", `{"prefix":{"addr":"fe80::","len":10}}`, `"ff02::1:2"`, "546", "547"}, {"10", `{"prefix":{"addr":"fe80::","len":10}}`, `{"prefix":{"addr":"fe80::","len":10}}`, "546", "547"}} {
		address := ""
		if ports.source != "" {
			address = fmt.Sprintf(`,{"match":{"op":"==","left":{"payload":{"protocol":"ip6","field":"saddr"}},"right":%s}},{"match":{"op":"==","left":{"payload":{"protocol":"ip6","field":"daddr"}},"right":%s}}`, ports.source, ports.destination)
		}
		expr := fmt.Sprintf(`[{"match":{"op":"!=","left":{"meta":{"key":"oifname"}},"right":%q}},{"match":{"op":"==","left":{"meta":{"key":"nfproto"}},"right":%s}},{"match":{"op":"==","left":{"meta":{"key":"l4proto"}},"right":17}}%s,{"match":{"op":"==","left":{"payload":{"protocol":"udp","field":"sport"}},"right":%s}},{"match":{"op":"==","left":{"payload":{"protocol":"udp","field":"dport"}},"right":%s}},{"accept":null}]`, device, ports.family, address, ports.sport, ports.dport)
		if !json.Valid([]byte(expr)) {
			panic("invalid DHCP fixture")
		}
		dhcp += fmt.Sprintf(`,{"rule":{"family":"inet","table":%q,"chain":"output","expr":%s}}`, table, expr)
	}
	extra = dhcp + extra
	return []byte(fmt.Sprintf(`{"nftables":[
{"metainfo":{"version":"1.1.5","release_name":"test fixture","json_schema_version":1}},
{"table":{"family":"inet","name":%[1]q,"handle":1}},
{"chain":{"family":"inet","table":%[1]q,"name":"output","handle":2,"type":"filter","hook":"output","prio":0,"policy":"drop"}},
{"rule":{"family":"inet","table":%[1]q,"chain":"output","handle":4,"expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"lo"}},{"accept":null}]}},
{"rule":{"family":"inet","table":%[1]q,"chain":"output","handle":5,"expr":[{"match":{"op":"==","left":{"meta":{"key":"mark"}},"right":%[3]d}},{"match":{"op":"==","left":{"meta":{"key":"l4proto"}},"right":{"set":[6,17]}}},{"accept":null}]}},
{"rule":{"family":"inet","table":%[1]q,"chain":"output","handle":6,"expr":[{"match":{"op":"!=","left":{"meta":{"key":"oifname"}},"right":%[2]q}},{"match":{"op":"==","left":{"payload":{"protocol":"ip6","field":"hoplimit"}},"right":255}},{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"type"}},"right":{"set":[133,135,136]}}},{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"code"}},"right":0}},{"accept":null}]}}%[4]s,
{"chain":{"family":"inet","table":%[1]q,"name":"forward","handle":3,"type":"filter","hook":"forward","prio":0,"policy":"drop"}}
]}`, table, device, mark, extra))
}

// Model command effects and answer actual readback commands. Existing engine
// fixtures still observe mutations; readback cannot succeed before a matching
// table transaction, or pretend that Release left the table present.
func exitGuardReadbackRunner(t *testing.T, device string, mark uint32, mutate commandInputRunner) commandInputRunner {
	t.Helper()
	scope := sha256.Sum256([]byte(device))
	table := fmt.Sprintf("endlessnet_exit_%x", scope[:12])
	present, open := false, false
	family := api.ExitFamilyDualStack
	return func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		if name != "nft" || reflect.DeepEqual(args, []string{"-f", "-"}) {
			raw, err := mutate(ctx, input, name, args...)
			if name == "nft" && err == nil {
				present = strings.Contains(input, "add chain inet "+table+" output")
				open = strings.Contains(input, fmt.Sprintf("oifname %q accept", device))
				family = api.ExitFamilyDualStack
				if strings.Contains(input, "forward meta nfproto ipv6 accept") {
					family = api.ExitFamilyIPv4Only
				}
				if strings.Contains(input, "forward meta nfproto ipv4 accept") {
					family = api.ExitFamilyIPv6Only
				}
			}
			return raw, err
		}
		if input != "" {
			t.Fatal("readback unexpectedly supplied mutations")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded readback")
		}
		if reflect.DeepEqual(args, []string{"-j", "-n", "list", "table", "inet", table}) {
			if !present {
				return nil, errors.New("table absent")
			}
			return exitGuardFamilyReadbackFixture(table, device, mark, open, family), nil
		}
		if reflect.DeepEqual(args, []string{"-j", "-n", "list", "tables"}) {
			if present {
				return []byte(fmt.Sprintf(`{"nftables":[{"metainfo":{"version":"1.1.5","release_name":"test fixture","json_schema_version":1}},{"table":{"family":"inet","name":%q,"handle":1}}]}`, table)), nil
			}
			return []byte(exitGuardEmptyTablesFixture), nil
		}
		t.Fatalf("unexpected nft observation command: %v", args)
		return nil, nil
	}
}

func TestExitGuardReadbackRejectsWideningAndMalformedRules(t *testing.T) {
	for _, open := range []bool{false, true} {
		good := string(exitGuardReadbackFixture("owned", "exit0", 51820, open))
		if !exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, open, api.ExitFamilyDualStack) {
			t.Fatal("valid numeric nft readback rejected")
		}
		for _, change := range []struct{ name, from, to string }{
			{"table_flags", `"name":"owned","handle":1`, `"name":"owned","handle":1,"flags":["dormant"]`},
			{"family", `"family":"inet"`, `"family":"ip"`},
			{"priority", `"prio":0`, `"prio":-1`},
			{"policy", `"policy":"drop"`, `"policy":"accept"`},
			{"hook", `"hook":"forward"`, `"hook":"input"`},
			{"unmarked", `"right":51820`, `"right":0`},
			{"protocol", `"set":[6,17]`, `"set":[1,6,17]`},
			{"icmp_redirect", `"set":[133,135,136]`, `"set":[133,135,136,137]`},
			{"hoplimit", `"right":255`, `"right":254`},
			{"code", `"field":"code"}},"right":0`, `"field":"code"}},"right":1`},
			{"interface", `"right":"exit0"`, `"right":"other"`},
			{"operator", `"op":"!="`, `"op":"=="`},
			{"verdict", `"accept":null`, `"continue":null`},
			{"duplicate_key", `"policy":"drop"`, `"policy":"accept","policy":"drop"`},
			{"chain_unknown", `"type":"filter"`, `"type":"filter","dev":"eth0"`},
			{"extra_statement", `{"accept":null}`, `{"counter":null},{"accept":null}`},
		} {
			t.Run(fmt.Sprintf("%t/%s", open, change.name), func(t *testing.T) {
				bad := strings.Replace(good, change.from, change.to, 1)
				if bad == good {
					t.Fatal("fixture mutation did not apply")
				}
				if exitGuardRulesObserved([]byte(bad), "owned", "exit0", 51820, open, api.ExitFamilyDualStack) {
					t.Fatal("ambiguous/widened readback accepted")
				}
			})
		}
		if exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, !open, api.ExitFamilyDualStack) {
			t.Fatal("wrong tunnel gate state accepted")
		}
	}
	for _, raw := range []string{"", `null`, `{"nftables":null}`, exitGuardEmptyTablesFixture + `{}`, strings.Repeat(" ", 1<<20) + exitGuardEmptyTablesFixture, `{"nftables":[{"metainfo":{"version":"nft","release_name":"fixture","json_schema_version":2}}]}`} {
		if exitGuardRulesObserved([]byte(raw), "owned", "exit0", 51820, false, api.ExitFamilyDualStack) || exitGuardTableAbsent([]byte(raw), "owned") {
			t.Fatal("malformed/unsupported/oversize JSON accepted")
		}
	}
}

func TestExitGuardReleaseRequiresObservedAbsence(t *testing.T) {
	if !exitGuardTableAbsent([]byte(exitGuardEmptyTablesFixture), "owned") {
		t.Fatal("successful empty listing rejected")
	}
	for _, tc := range []struct {
		entry  string
		absent bool
	}{
		{`{"table":{"family":"inet","name":"other","handle":1}}`, true},
		{`{"table":{"family":"inet","name":"other","handle":1,"comment":"unrelated policy","flags":"dormant"}}`, true},
		{`{"table":{"family":"ip","name":"owned","handle":1}}`, true},
		{`{"table":{"family":"inet","name":"owned","handle":1}}`, false},
		{`{"chain":{"family":"inet","table":"other"}}`, false},
		{`{"table":{"family":"inet","name":null}}`, false},
		{`{"table":{"family":"inet","name":"other","unknown":true}}`, false},
	} {
		raw := strings.TrimSuffix(exitGuardEmptyTablesFixture, "]}") + "," + tc.entry + "]}"
		if got := exitGuardTableAbsent([]byte(raw), "owned"); got != tc.absent {
			t.Fatalf("absence=%t for %s", got, tc.entry)
		}
	}
}

func TestExitGuardReadbackAcceptsOnlyExactRestrictiveNDDependencies(t *testing.T) {
	base := string(exitGuardReadbackFixture("owned", "exit0", 51820, false))
	needle := `{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"type"}}`
	for _, left := range []string{`{"meta":{"key":"nfproto"}}`, `{"meta":{"key":"l4proto"}}`, `{"payload":{"protocol":"ip6","field":"nexthdr"}}`} {
		value := "58"
		if strings.Contains(left, "nfproto") {
			value = "10"
		}
		predicate := fmt.Sprintf(`{"match":{"op":"==","left":%s,"right":%s}},`, left, value)
		good := strings.Replace(base, needle, predicate+needle, 1)
		if good == base || !exitGuardRulesObserved([]byte(good), "owned", "exit0", 51820, false, api.ExitFamilyDualStack) {
			t.Fatal("exact protocol prerequisite rejected", left)
		}
		for _, bad := range []string{
			strings.Replace(base, needle, predicate+predicate+needle, 1),
			strings.Replace(base, needle, strings.Replace(predicate, `"op":"=="`, `"op":"!="`, 1)+needle, 1),
			strings.Replace(base, needle, strings.Replace(predicate, `"right":`+value, `"right":0`, 1)+needle, 1),
		} {
			if exitGuardRulesObserved([]byte(bad), "owned", "exit0", 51820, false, api.ExitFamilyDualStack) {
				t.Fatal("noncanonical prerequisite ignored", left)
			}
		}
	}
}

func TestExitGuardNativeSuccessRequiresReadbackAndRecoversAmbiguity(t *testing.T) {
	for _, action := range []string{"contain", "open", "release"} {
		for _, fault := range []string{"command_error", "malformed", "wrong_state", "cancelled", "recovery_error"} {
			t.Run(action+"/"+fault, func(t *testing.T) {
				writes, reads := 0, 0
				armed, failed := false, false
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				base := exitGuardReadbackRunner(t, "exit0", 51820, func(call context.Context, input, name string, args ...string) ([]byte, error) {
					writes++
					if _, ok := call.Deadline(); !ok {
						t.Fatal("unbounded transaction")
					}
					if failed && action != "contain" {
						if call.Err() != nil {
							t.Fatal("recovery reused cancelled operation context")
						}
						if strings.Contains(input, `output oifname "exit0" accept`) {
							t.Fatal("recovery reopened TUN")
						}
						if fault == "recovery_error" {
							return []byte("private host details"), errors.New("private host details")
						}
					}
					return nil, nil
				})
				var guard *linuxExitGuard
				runner := func(call context.Context, input, name string, args ...string) ([]byte, error) {
					raw, err := base(call, input, name, args...)
					if len(args) > 0 && args[0] == "-j" {
						reads++
						if armed && !failed {
							failed = true
							switch fault {
							case "command_error", "recovery_error":
								return []byte("private host details"), errors.New("private host details")
							case "malformed":
								return []byte(`{"nftables":null}`), nil
							case "wrong_state":
								if action == "release" {
									return []byte(fmt.Sprintf(`{"nftables":[{"metainfo":{"version":"1.1.5","release_name":"test","json_schema_version":1}},{"table":{"family":"inet","name":%q}}]}`, guard.table)), nil
								}
								return exitGuardReadbackFixture(guard.table, "exit0", 51820, action == "contain"), nil
							case "cancelled":
								cancel()
							}
						}
					}
					return raw, err
				}
				var err error
				guard, err = newLinuxExitGuard("exit0", 51820, runner)
				if err != nil {
					t.Fatal(err)
				}
				if err := guard.Contain(ctx); err != nil {
					t.Fatal(err)
				}
				armed = true
				switch action {
				case "contain":
					err = guard.Contain(ctx)
				case "open":
					err = guard.OpenTunnel(ctx, api.ExitFamilyDualStack)
				case "release":
					err = guard.Release(ctx)
				}
				if err == nil || strings.Contains(err.Error(), "private host details") {
					t.Fatal("readback error became success or disclosed command output", err)
				}
				if fault == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("lost caller cancellation", err)
				}
				wantWrites, wantReads := 3, 3
				if action == "contain" {
					wantWrites, wantReads = 2, 2
				} else if fault == "recovery_error" {
					wantReads = 2
				}
				if writes != wantWrites || reads != wantReads {
					t.Fatalf("transactions=%d readbacks=%d; expected %d/%d", writes, reads, wantWrites, wantReads)
				}
			})
		}
	}
}
