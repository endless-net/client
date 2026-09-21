package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type underlayDNSSourceFixture struct {
	interfaces   []any
	manager      map[string]any
	link         map[string]any
	mutate       func(pass int)
	ownerChanged bool
	reads        int
	owners       int
	pass         int
}

func dnsSourceVariant(signature string, data any) any {
	return map[string]any{"type": signature, "data": data}
}

func newUnderlayDNSSourceFixture() *underlayDNSSourceFixture {
	server := []any{2, []any{192, 0, 2, 53}, 0, ""}
	domains := []any{[]any{"corp.example", true}}
	return &underlayDNSSourceFixture{
		interfaces: []any{
			map[string]any{"ifindex": 1, "ifname": "lo", "flags": []any{"UP", "LOOPBACK"}, "addr_info": []any{map[string]any{"local": "127.0.0.1"}}},
			map[string]any{"ifindex": 2, "ifname": "eth0", "flags": []any{"UP", "BROADCAST"}, "addr_info": []any{map[string]any{"local": "192.0.2.2"}}},
			map[string]any{"ifindex": 9, "ifname": "endlessnet", "flags": []any{"UP"}, "addr_info": []any{map[string]any{"local": "100.64.0.1"}}},
		},
		manager: map[string]any{
			"DNSEx":   dnsSourceVariant("a(iiayqs)", []any{append([]any{2}, server...), []any{9, 2, []any{100, 64, 0, 1}, 53, ""}}),
			"Domains": dnsSourceVariant("a(isb)", []any{[]any{2, "corp.example", true}, []any{9, ".", true}}),
			"DNSSEC":  dnsSourceVariant("s", "no"), "DNSOverTLS": dnsSourceVariant("s", "no"),
			// This public fallback must never become a captured endpoint.
			"FallbackDNSEx": dnsSourceVariant("a(iiayqs)", []any{[]any{0, 2, []any{203, 0, 113, 53}, 53, ""}}),
		},
		link: map[string]any{"DNSEx": dnsSourceVariant("a(iayqs)", []any{server}), "Domains": dnsSourceVariant("a(sb)", domains), "ScopesMask": dnsSourceVariant("t", 1), "DefaultRoute": dnsSourceVariant("b", true), "DNSSEC": dnsSourceVariant("s", "no"), "DNSOverTLS": dnsSourceVariant("s", "no")},
	}
}

func (f *underlayDNSSourceFixture) runner(t *testing.T) CommandRunner {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.reads++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("source command has no bounded deadline")
		}
		var value any
		switch name {
		case "ip":
			if !slices.Equal(args, []string{"-j", "address", "show"}) {
				t.Fatal("unexpected source command", args)
			}
			f.pass++
			if f.mutate != nil {
				f.mutate(f.pass)
			}
			value = f.interfaces
		case "busctl":
			prefix := []string{"--system", "--json=short", "--timeout=5s", "--auto-start=no", "--allow-interactive-authorization=no", "call"}
			if len(args) != 12 || !slices.Equal(args[:6], prefix) {
				t.Fatal("unbounded/interactive/unexpected bus command", args)
			}
			switch args[9] {
			case "GetNameOwner":
				if args[6] != "org.freedesktop.DBus" || args[11] != "org.freedesktop.resolve1" {
					t.Fatal("wrong owner query")
				}
				f.owners++
				owner := ":1.17"
				if f.ownerChanged && f.owners > 1 {
					owner = ":1.18"
				}
				value = dnsSourceVariant("s", []any{owner})
			case "GetLink":
				if args[6] != ":1.17" || args[11] != "2" {
					t.Fatal("queried own or unexpected link / unpinned owner", args)
				}
				value = dnsSourceVariant("o", []any{resolveManagerPath + "/link/_2"})
			case "GetAll":
				if args[6] != ":1.17" {
					t.Fatal("properties queried through replaceable well-known owner")
				}
				properties := f.manager
				if args[7] != resolveManagerPath {
					properties = f.link
				}
				value = dnsSourceVariant("a{sv}", []any{properties})
			default:
				t.Fatal("unexpected D-Bus method", args)
			}
		default:
			t.Fatal("unexpected command", name)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw, nil
	}
}

func TestUnderlayDNSSourceCapturesOnlyBoundNonClientLinks(t *testing.T) {
	f := newUnderlayDNSSourceFixture()
	source, err := captureUnderlayDNSSource(t.Context(), "endlessnet", f.runner(t))
	if err != nil {
		t.Fatal(err)
	}
	want := &underlayDNSSource{Owner: ":1.17", Links: []underlayDNSLink{{Index: 2, Name: "eth0", Servers: []netip.AddrPort{netip.MustParseAddrPort("192.0.2.53:53")}, Domains: []underlayDNSDomain{{Name: "corp.example", RouteOnly: true}}, DefaultRoute: true, DNSSEC: "no", DNSOverTLS: "no"}}}
	want.Interfaces = []underlayDNSInterface{
		{Index: 1, Name: "lo", Up: true, Loopback: true, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Index: 2, Name: "eth0", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.2")}},
	}
	if !reflect.DeepEqual(source, want) || f.pass != 2 || f.owners != 2 {
		t.Fatal("wrong normalized source", source)
	}
	clone := cloneUnderlayDNSSource(source)
	clone.Links[0].Servers[0] = netip.MustParseAddrPort("192.0.2.54:53")
	clone.Links[0].Domains[0].Name = "other.example"
	clone.Interfaces[1].Addresses[0] = netip.MustParseAddr("192.0.2.3")
	if !reflect.DeepEqual(source, want) {
		t.Fatal("source aliases clone")
	}
}

func TestUnderlayDNSSourceRejectsAmbiguousAndUnsafeSources(t *testing.T) {
	for _, scenario := range []string{"stub", "global_stub", "fallback_only", "client_address", "multicast", "tls", "dnssec", "inherit_unknown", "missing_default", "link_mismatch", "missing_interface", "duplicate_interface", "bad_domain", "duplicate_domain", "bad_port", "sni", "bad_family", "down", "inactive_scope", "owner_changed", "source_changed", "interface_changed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUnderlayDNSSourceFixture()
			changeServer := func(server []any) {
				f.link["DNSEx"] = dnsSourceVariant("a(iayqs)", []any{server})
				f.manager["DNSEx"] = dnsSourceVariant("a(iiayqs)", []any{append([]any{2}, server...)})
			}
			switch scenario {
			case "stub":
				changeServer([]any{2, []any{127, 0, 0, 53}, 53, ""})
			case "global_stub":
				f.manager["DNSEx"] = dnsSourceVariant("a(iiayqs)", []any{[]any{0, 2, []any{127, 0, 0, 53}, 53, ""}})
			case "fallback_only":
				f.manager["DNSEx"] = dnsSourceVariant("a(iiayqs)", []any{})
			case "client_address":
				changeServer([]any{2, []any{100, 64, 0, 1}, 53, ""})
			case "multicast":
				changeServer([]any{2, []any{224, 0, 0, 1}, 53, ""})
			case "tls":
				f.link["DNSOverTLS"] = dnsSourceVariant("s", "opportunistic")
			case "dnssec":
				f.link["DNSSEC"] = dnsSourceVariant("s", "allow-downgrade")
			case "inherit_unknown":
				f.link["DNSSEC"] = dnsSourceVariant("s", "")
			case "missing_default":
				delete(f.link, "DefaultRoute")
			case "link_mismatch":
				f.link["DNSEx"] = dnsSourceVariant("a(iayqs)", []any{})
			case "missing_interface":
				f.interfaces = append(f.interfaces[:1], f.interfaces[2:]...)
			case "duplicate_interface":
				f.interfaces = append(f.interfaces, f.interfaces[1])
			case "bad_domain":
				f.link["Domains"] = dnsSourceVariant("a(sb)", []any{[]any{"*.example", true}})
				f.manager["Domains"] = dnsSourceVariant("a(isb)", []any{[]any{2, "*.example", true}})
			case "duplicate_domain":
				f.link["Domains"] = dnsSourceVariant("a(sb)", []any{[]any{"corp.example", true}, []any{"CORP.EXAMPLE.", false}})
				f.manager["Domains"] = dnsSourceVariant("a(isb)", []any{[]any{2, "corp.example", true}, []any{2, "CORP.EXAMPLE.", false}})
			case "bad_port":
				changeServer([]any{2, []any{192, 0, 2, 53}, 65536, ""})
			case "sni":
				changeServer([]any{2, []any{192, 0, 2, 53}, 853, "dns.example"})
			case "bad_family":
				changeServer([]any{3, []any{192, 0, 2, 53}, 53, ""})
			case "down":
				f.interfaces[1].(map[string]any)["flags"] = []any{"BROADCAST"}
			case "inactive_scope":
				f.link["ScopesMask"] = dnsSourceVariant("t", 0)
			case "owner_changed":
				f.ownerChanged = true
			case "source_changed":
				f.mutate = func(pass int) {
					if pass == 2 {
						changeServer([]any{2, []any{192, 0, 2, 54}, 53, ""})
					}
				}
			case "interface_changed":
				f.mutate = func(pass int) {
					if pass == 2 {
						f.interfaces[1].(map[string]any)["addr_info"] = []any{map[string]any{"local": "192.0.2.3"}}
					}
				}
			}
			if source, err := captureUnderlayDNSSource(t.Context(), "endlessnet", f.runner(t)); err == nil || source != nil {
				t.Fatal("unsafe or inconsistent observation published", source)
			}
		})
	}
}

func TestUnderlayDNSSourceBoundsAndCancellation(t *testing.T) {
	for _, scenario := range []string{"cancel_before", "cancel_during", "oversize", "duplicate_json", "trailing_json", "command_error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reads := 0
			if scenario == "cancel_before" {
				cancel()
			}
			runner := func(context.Context, string, ...string) ([]byte, error) {
				reads++
				switch scenario {
				case "cancel_during":
					cancel()
					return []byte(`{"type":"s","data":[":1.17"]}`), nil
				case "oversize":
					return []byte(strings.Repeat(" ", 1<<20+1)), nil
				case "duplicate_json":
					return []byte(`{"type":"s","type":"s","data":[":1.17"]}`), nil
				case "trailing_json":
					return []byte(`{"type":"s","data":[":1.17"]}{}`), nil
				default:
					return nil, errors.New("private command output must not leak")
				}
			}
			source, err := captureUnderlayDNSSource(ctx, "endlessnet", runner)
			if source != nil || err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid source error", source, err)
			}
			if strings.HasPrefix(scenario, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if scenario == "cancel_before" && reads != 0 {
				t.Fatal("cancelled capture dispatched native command")
			}
		})
	}
}

func TestUnderlayDNSSourcePreservesGlobalAndScopedServers(t *testing.T) {
	f := newUnderlayDNSSourceFixture()
	// Manager DNSEx includes only explicitly configured sources here. A
	// configured global resolver is distinct from FallbackDNSEx.
	global := []any{0, 2, []any{198, 51, 100, 53}, 5353, ""}
	scoped := []any{10, []any{254, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 53}, 53, ""}
	f.manager["DNSEx"] = dnsSourceVariant("a(iiayqs)", []any{global, append([]any{2}, scoped...)})
	f.link["DNSEx"] = dnsSourceVariant("a(iayqs)", []any{scoped})
	f.manager["Domains"] = dnsSourceVariant("a(isb)", []any{[]any{2, "corp.example", true}, []any{0, "PUBLIC.EXAMPLE.", false}, []any{9, ".", true}})
	source, err := captureUnderlayDNSSource(t.Context(), "endlessnet", f.runner(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Links) != 2 || source.Links[0].Index != 0 || source.Links[0].Name != "" || source.Links[0].Servers[0] != netip.MustParseAddrPort("198.51.100.53:5353") || source.Links[0].Domains[0] != (underlayDNSDomain{Name: "public.example", RouteOnly: false}) || source.Links[1].Servers[0] != netip.MustParseAddrPort("[fe80::35%eth0]:53") {
		t.Fatal("global or scoped source lost", source)
	}
}

func TestUnderlayDNSSourceNoServersDoesNotReserveDomain(t *testing.T) {
	f := newUnderlayDNSSourceFixture()
	// A non-Client interface with a domain but no nameserver cannot supply
	// queries in resolved. Capture must not fabricate its resolver from global.
	f.interfaces = append(f.interfaces, map[string]any{"ifindex": 3, "ifname": "eth1", "flags": []any{"UP"}, "addr_info": []any{}})
	f.manager["Domains"] = dnsSourceVariant("a(isb)", []any{[]any{2, "corp.example", true}, []any{3, "private.example", true}})
	source, err := captureUnderlayDNSSource(t.Context(), "endlessnet", f.runner(t))
	if err != nil || len(source.Links) != 1 || source.Links[0].Name != "eth0" {
		t.Fatal("empty DNS link became a usable source", source, err)
	}
}

func TestUnderlayDNSSourceRejectsAmbiguousOwnInterface(t *testing.T) {
	for _, name := range []string{"", "lo", " endlessnet", "endlessnet ", "endlessnet/other"} {
		called := false
		_, err := captureUnderlayDNSSource(t.Context(), name, func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil })
		if err == nil || called {
			t.Fatal("invalid own interface dispatched discovery", name)
		}
	}
}
