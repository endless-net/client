package client

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestLinuxRouterCleanupRetriesFailuresWithoutReapplying(t *testing.T) {
	for _, failureKind := range []string{"route", "dns", "interface"} {
		t.Run(failureKind, func(t *testing.T) {
			cfg := wireGuardEngineRouterConfig{Interface: "endlessnet", MTU: 1280, Routes: []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")}, DNS: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
			calls := map[string]int{}
			failing := true
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				kind := "interface"
				if name == "resolvectl" {
					kind = "dns"
				} else if len(args) > 1 && args[1] == "route" {
					kind = "route"
				}
				key := name + " " + strings.Join(args, " ")
				calls[key]++
				if failing && kind == failureKind {
					return nil, errors.New("injected removal failure")
				}
				return nil, nil
			}
			r := &linuxWireGuardEngineRouter{interfaceName: cfg.Interface, configured: true, current: cfg, runner: run, interfacePresent: func(string) (bool, error) { return true, nil }}
			if err := r.Down(t.Context()); err == nil || !r.configured || r.pendingCleanup == nil {
				t.Fatal("failed cleanup lost retry state", err)
			}
			before := make(map[string]int, len(calls))
			for key, count := range calls {
				before[key] = count
			}
			if err := r.Configure(t.Context(), cfg); err == nil {
				t.Fatal("Configure bypassed pending cleanup")
			}
			for key, count := range calls {
				if count > before[key] && (strings.Contains(key, "route replace") || strings.Contains(key, " mtu ")) {
					t.Fatal("cleanup retry applied networking", key)
				}
			}
			failing = false
			if err := r.Down(t.Context()); err != nil || r.configured || r.pendingCleanup != nil {
				t.Fatal("retry did not complete cleanup", err)
			}
			for key, count := range calls {
				kind := "interface"
				if strings.HasPrefix(key, "resolvectl ") {
					kind = "dns"
				} else if strings.Contains(key, " route ") {
					kind = "route"
				}
				want := 1
				if kind == failureKind {
					want = 3
				}
				if count != want {
					t.Fatal("cleanup repeated completed step or lost failed step", key, count, want)
				}
			}
		})
	}
}

func TestLinuxExitRouteRequiresObservedRuleRemovalBeforeAddingRules(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		for _, stage := range []string{"dedicated", "suppression"} {
			for _, outcome := range []string{"absent", "remaining", "failed"} {
				t.Run(family+"/"+stage+"/"+outcome, func(t *testing.T) {
					added, inspected := 0, 0
					r := &linuxWireGuardEngineRouter{runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
						command := name + " " + strings.Join(args, " ")
						if strings.Contains(command, "rule add") {
							if strings.Contains(command, "suppress_prefixlength") && !strings.Contains(command, "not fwmark 51820 table main suppress_prefixlength 0") {
								t.Fatal("suppression rule lacks ownership", command)
							}
							added++
						}
						if strings.Contains(command, "rule del") {
							return nil, errors.New("ambiguous delete")
						}
						if strings.Contains(command, "rule show") {
							inspected++
							if args[0] != family {
								t.Fatal("rule observation changed IP family")
							}
							target := args[len(args)-1]
							if (stage == "dedicated" && target == "51820") || (stage == "suppression" && target == "254") {
								switch outcome {
								case "failed":
									return nil, errors.New("observation failed")
								case "remaining":
									return ownedPolicyRuleFixture(51820, target == "254"), nil
								}
							}
							return []byte(`[]`), nil
						}
						return nil, nil
					}}
					route := netip.MustParsePrefix("0.0.0.0/0")
					if family == "-6" {
						route = netip.MustParsePrefix("::/0")
					}
					err := r.addRoute(t.Context(), wireGuardEngineRouterConfig{Interface: "endlessnet", FirewallMark: 51820}, route)
					if outcome == "absent" {
						if err != nil || inspected != 2 || added != 2 {
							t.Fatal("confirmed absence did not permit rule installation", err, inspected, added)
						}
					} else if err == nil || added != 0 {
						t.Fatal("unconfirmed removal permitted rule installation", err, added)
					}
				})
			}
		}
	}
}

func TestLinuxRouterRejectsUnclearedAddressesBeforeNewConfiguration(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		t.Run(family, func(t *testing.T) {
			cfg := wireGuardEngineRouterConfig{Interface: "endlessnet", MTU: 1280,
				Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.2/32")},
				Routes:    []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")},
				DNS:       []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
			failure := errors.New("old address flush failed")
			failing := true
			applied := 0
			flushes := 0
			r := &linuxWireGuardEngineRouter{interfaceName: cfg.Interface,
				interfacePresent: func(string) (bool, error) { return true, nil },
				runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
					command := name + " " + strings.Join(args, " ")
					if strings.Contains(command, "addr add") || strings.Contains(command, "route replace") || strings.HasPrefix(command, "resolvectl dns") {
						applied++
					}
					if command == "ip "+family+" addr flush dev endlessnet scope global" {
						flushes++
						if failing {
							return nil, failure
						}
					}
					return nil, nil
				}}
			if err := r.Configure(t.Context(), cfg); err == nil || applied != 0 || !r.configured || r.pendingCleanup == nil {
				t.Fatal("failed address cleanup allowed new configuration or lost retry state", err)
			}
			if err := r.Configure(t.Context(), cfg); err == nil || applied != 0 {
				t.Fatal("new attempt bypassed failed cleanup", err)
			}
			before := flushes
			failing = false
			if err := r.Configure(t.Context(), cfg); err != nil || applied != 3 || r.pendingCleanup != nil || flushes != before+2 {
				t.Fatal("successful cleanup did not precede reapplication", err, applied, flushes)
			}
		})
	}
}
