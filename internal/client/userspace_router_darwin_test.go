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
