package client

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestDarwinTUNAddressesIncludeIPv4PointToPointDestination(t *testing.T) {
	var commands []string
	r := &darwinWireGuardEngineRouter{runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}}
	err := r.Configure(t.Context(), wireGuardEngineRouterConfig{
		Interface: "utun99", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("100.90.0.2/32"), netip.MustParsePrefix("fd00::2/128")},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ifconfig utun99 mtu 1280 up", "ifconfig utun99 inet 100.90.0.2/32 100.90.0.2 alias", "ifconfig utun99 inet6 fd00::2/128 alias"}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected TUN address commands: %v", commands)
	}
}

func TestDarwinDefaultRoutesPreservePhysicalInterfaceDefaults(t *testing.T) {
	var commands []string
	r := &darwinWireGuardEngineRouter{runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}}
	cfg := wireGuardEngineRouterConfig{
		Interface: "utun99", MTU: 1280,
		Routes: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
	}
	if err := r.Configure(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ifconfig utun99 mtu 1280 up",
		"route -n add -inet 0.0.0.0/1 -interface utun99",
		"route -n add -inet 128.0.0.0/1 -interface utun99",
		"route -n add -inet6 ::/1 -interface utun99",
		"route -n add -inet6 8000::/1 -interface utun99",
		"route -n delete -inet 0.0.0.0/1 -interface utun99",
		"route -n delete -inet 128.0.0.0/1 -interface utun99",
		"route -n delete -inet6 ::/1 -interface utun99",
		"route -n delete -inet6 8000::/1 -interface utun99",
		"ifconfig utun99 down",
	}
	if !slices.Equal(commands, want) {
		t.Fatalf("default route lifecycle disturbed physical defaults: %v", commands)
	}
}

func TestDarwinOverlappingDefaultRouteLifecycle(t *testing.T) {
	for _, prefixes := range [][]string{{"0.0.0.0/0", "0.0.0.0/1", "128.0.0.0/1"}, {"::/0", "::/1", "8000::/1"}} {
		t.Run(prefixes[0], func(t *testing.T) {
			installed := map[string]bool{}
			r := &darwinWireGuardEngineRouter{runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ifconfig" {
					return nil, nil
				}
				prefix := args[3]
				if prefix == prefixes[0] {
					t.Fatal("attempted to modify system default")
				}
				if args[1] == "add" {
					if installed[prefix] {
						return nil, errors.New("duplicate route")
					}
					installed[prefix] = true
				} else {
					if !installed[prefix] {
						return nil, errors.New("absent route")
					}
					delete(installed, prefix)
				}
				return nil, nil
			}}
			for _, step := range []struct {
				routes []string
				want   []string
			}{
				{prefixes[:2], prefixes[1:]},
				{prefixes[1:2], prefixes[1:2]},
				{prefixes[:1], prefixes[1:]},
				{prefixes[:2], prefixes[1:]},
				{nil, nil},
			} {
				cfg := wireGuardEngineRouterConfig{Interface: "utun99", MTU: 1280}
				for _, value := range step.routes {
					cfg.Routes = append(cfg.Routes, netip.MustParsePrefix(value))
				}
				if err := r.Configure(t.Context(), cfg); err != nil {
					t.Fatal(err)
				}
				if len(installed) != len(step.want) {
					t.Fatalf("routes=%v want=%v", installed, step.want)
				}
				for _, value := range step.want {
					if !installed[value] {
						t.Fatalf("missing retained route %s", value)
					}
				}
			}
		})
	}
}

func TestDarwinFailedRollbackRetainsRoutesForRecovery(t *testing.T) {
	original := wireGuardEngineRouterConfig{Interface: "utun99", MTU: 1280}
	installed := map[string]bool{}
	fail := true
	r := &darwinWireGuardEngineRouter{configured: true, current: original, runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "route" {
			t.Fatal("route recovery disturbed interface")
		}
		if fail && (args[3] == "128.0.0.0/1" || args[1] == "delete") {
			return nil, errors.New("injected route failure")
		}
		if args[1] == "add" {
			installed[args[3]] = true
		} else {
			delete(installed, args[3])
		}
		return nil, nil
	}}
	next := original
	next.Routes = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	if err := r.Configure(t.Context(), next); err == nil {
		t.Fatal("failed rollback was accepted")
	}
	if len(installed) != 1 || !installed["0.0.0.0/1"] {
		t.Fatalf("unexpected partial routes: %v", installed)
	}
	fail = false
	if err := r.Configure(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("recovery leaked routes after failed rollback: %v", installed)
	}
}

func TestDarwinSetupFailurePreservesUnownedRoutes(t *testing.T) {
	for _, failure := range []string{"file exists", "permission denied"} {
		t.Run(failure, func(t *testing.T) {
			installed := map[string]string{"128.0.0.0/1": "physical"}
			r := &darwinWireGuardEngineRouter{runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "ifconfig" {
					return nil, nil
				}
				if name != "route" || len(args) != 6 {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				prefix := args[3]
				if args[1] == "add" {
					if prefix == "128.0.0.0/1" {
						return []byte(failure), errors.New(failure)
					}
					installed[prefix] = "client"
				} else {
					if installed[prefix] != "client" {
						t.Fatalf("cleanup attempted to remove unowned route %s", prefix)
					}
					delete(installed, prefix)
				}
				return nil, nil
			}}
			cfg := wireGuardEngineRouterConfig{Interface: "utun99", MTU: 1280,
				Routes: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("203.0.113.0/24")}}
			if err := r.Configure(t.Context(), cfg); err == nil || !strings.Contains(err.Error(), failure) {
				t.Fatalf("setup failure was not propagated: %v", err)
			}
			if r.configured || len(installed) != 1 || installed["128.0.0.0/1"] != "physical" {
				t.Fatalf("failed setup leaked or removed routes: %v", installed)
			}
		})
	}
}

func TestDarwinDefaultRoutePartialFailureRestoresRouteSet(t *testing.T) {
	for _, operation := range []string{"add", "delete"} {
		t.Run(operation, func(t *testing.T) {
			original := wireGuardEngineRouterConfig{Interface: "utun99", MTU: 1280}
			next := cloneWireGuardEngineRouterConfig(original)
			next.Routes = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
			installed := map[string]bool{}
			if operation == "delete" {
				original, next = next, original
				installed["0.0.0.0/1"], installed["128.0.0.0/1"] = true, true
			}
			fail := true
			r := &darwinWireGuardEngineRouter{configured: true, current: cloneWireGuardEngineRouterConfig(original), runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "route" || len(args) != 6 {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				if fail && args[1] == operation && args[3] == "128.0.0.0/1" {
					return nil, errors.New("second half rejected")
				}
				if args[1] == "add" {
					installed[args[3]] = true
				} else {
					delete(installed, args[3])
				}
				return nil, nil
			}}
			if err := r.Configure(t.Context(), next); err == nil {
				t.Fatal("partial route failure was accepted")
			}
			assertRoutes := func(cfg wireGuardEngineRouterConfig) {
				t.Helper()
				want := darwinSystemRoutes(cfg.Routes)
				if len(installed) != len(want) {
					t.Fatalf("route set after operation: %v; want %v", installed, want)
				}
				for _, route := range want {
					if !installed[route.String()] {
						t.Fatalf("missing route %s", route)
					}
				}
			}
			assertRoutes(original)
			if !wireGuardEngineRouterConfigsEqual(r.current, original) {
				t.Fatal("failed operation changed committed configuration")
			}
			fail = false
			if err := r.Configure(t.Context(), next); err != nil {
				t.Fatal(err)
			}
			assertRoutes(next)
		})
	}
}

func TestDarwinUserspaceDNSPreservesScopedAndSearchDomains(t *testing.T) {
	script := darwinUserspaceDNSCommands(wireGuardEngineRouterConfig{
		Interface:        "utun99",
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSDomains:       []string{"corp.example", "nodes.example"},
		SearchDomains:    []string{"nodes.example"},
		DNSConfigPresent: true,
	})
	for _, want := range []string{
		"d.add SupplementalMatchDomains * corp.example nodes.example",
		"d.add SearchDomains * nodes.example",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("scoped DNS script missing %q:\n%s", want, script)
		}
	}
}

func TestDarwinUserspaceDNSUsesDefaultRouteForOverride(t *testing.T) {
	script := darwinUserspaceDNSCommands(wireGuardEngineRouterConfig{
		Interface:        "utun99",
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSOverride:      true,
		DNSConfigPresent: true,
	})
	if !strings.Contains(script, "d.add SupplementalMatchDomains * .") {
		t.Fatalf("override DNS script omitted the default route:\n%s", script)
	}
}

func TestDarwinRouteUpdatePreservesLiveInterfaceAndSupportsRollback(t *testing.T) {
	original := wireGuardEngineRouterConfig{
		Interface: "utun99", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.1/32")},
		Routes:    []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")},
	}
	next := cloneWireGuardEngineRouterConfig(original)
	next.Routes = append(next.Routes, netip.MustParsePrefix("198.18.94.21/32"), netip.MustParsePrefix("fd94::20/128"))
	for _, failSecondAdd := range []bool{false, true} {
		var commands []string
		r := &darwinWireGuardEngineRouter{configured: true, current: cloneWireGuardEngineRouterConfig(original), runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			if name != "route" {
				t.Fatalf("route update disrupted interface or address state: %s", command)
			}
			if failSecondAdd && command == "route -n add -inet6 fd94::20/128 -interface utun99" {
				return nil, errors.New("injected route failure")
			}
			return nil, nil
		}}
		err := r.Configure(t.Context(), next)
		if (err != nil) != failSecondAdd {
			t.Fatalf("route update error=%v, injected failure=%t", err, failSecondAdd)
		}
		if err := r.Configure(t.Context(), original); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"route -n add -inet 198.18.94.21/32 -interface utun99",
			"route -n add -inet6 fd94::20/128 -interface utun99",
			"route -n delete -inet 198.18.94.21/32 -interface utun99",
		}
		if !failSecondAdd {
			want = append(want, "route -n delete -inet6 fd94::20/128 -interface utun99")
		}
		if !slices.Equal(commands, want) || !wireGuardEngineRouterConfigsEqual(r.current, original) || !r.configured {
			t.Fatalf("route rollback did not preserve the original live interface: %v", commands)
		}
	}
}

func TestDarwinDNSProjectionUpdateDoesNotRecreateInterface(t *testing.T) {
	original := wireGuardEngineRouterConfig{
		Interface: "utun99", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.1/32")},
		Routes:    []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")},
		DNSProxy:  &DNSProxyOptions{ListenAddr: "127.0.0.1:53", SearchDomain: "scenario.endlessnet"},
	}
	next := cloneWireGuardEngineRouterConfig(original)
	next.DNSProxy.NetworkMap.Revision.Network = 2
	var commands []string
	r := &darwinWireGuardEngineRouter{interfaceName: original.Interface, configured: true, current: original,
		runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		}, inputRunner: func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		}}
	if err := r.Configure(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 || !wireGuardEnginePlatformRouterConfigsEqual(r.current, original) {
		t.Fatal("DNS projection update disturbed Darwin interface state")
	}
}
