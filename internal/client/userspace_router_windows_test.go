package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"net/netip"
)

func TestWindowsUserspaceRouterUsesNRPTForScopedDNS(t *testing.T) {
	script := windowsUserspaceRouterScript(wireGuardEngineRouterConfig{
		Interface:        "EndlessNet",
		MTU:              1280,
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSDomains:       []string{"corp.example", "nodes.example"},
		SearchDomains:    []string{"nodes.example"},
		DNSConfigPresent: true,
	}, false)
	for _, want := range []string{
		"Add-DnsClientNrptRule",
		"'.corp.example'",
		"'.nodes.example'",
		"ConnectionSpecificSuffix 'nodes.example'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("scoped DNS script missing %q: %s", want, script)
		}
	}
	if strings.Contains(script, "Set-DnsClientServerAddress -InterfaceAlias $ifName -ServerAddresses") {
		t.Fatalf("scoped DNS unexpectedly replaced the interface resolver: %s", script)
	}
}

func TestWindowsUserspaceRouterUsesInterfaceDNSForOverride(t *testing.T) {
	script := windowsUserspaceRouterScript(wireGuardEngineRouterConfig{
		Interface:        "EndlessNet",
		MTU:              1280,
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSOverride:      true,
		DNSConfigPresent: true,
	}, false)
	if !strings.Contains(script, "Set-DnsClientServerAddress -InterfaceAlias $ifName -ServerAddresses @('127.0.0.1')") {
		t.Fatalf("override DNS script did not replace the interface resolver: %s", script)
	}
}

func TestWindowsRouteUpdatesPreserveAddressesAndRetainedRoutes(t *testing.T) {
	original := wireGuardEngineRouterConfig{Interface: "EndlessNet", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.1/32")},
		Routes:    []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")}}
	next := cloneWireGuardEngineRouterConfig(original)
	next.Routes = append(next.Routes, netip.MustParsePrefix("198.18.94.21/32"), netip.MustParsePrefix("fd94::20/128"))
	var scripts []string
	r := &windowsWireGuardEngineRouter{configured: true, current: original, runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		script := args[len(args)-1]
		scripts = append(scripts, script)
		for _, forbidden := range []string{"NetIPAddress", "NetIPInterface", "DnsClient", "'198.18.94.20/32'", "Get-NetRoute"} {
			if strings.Contains(script, forbidden) {
				t.Fatalf("route delta disturbed existing interface state: %s", forbidden)
			}
		}
		return nil, nil
	}}
	if err := r.Configure(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if err := r.Configure(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if err := r.Configure(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 2 || !strings.Contains(scripts[0], "New-NetRoute -DestinationPrefix '198.18.94.21/32'") || !strings.Contains(scripts[0], "-NextHop '::'") || !strings.Contains(scripts[1], "Remove-NetRoute -DestinationPrefix 'fd94::20/128'") || !wireGuardEngineRouterConfigsEqual(r.current, original) {
		t.Fatal("route delta did not add, remove and preserve the required routes")
	}
}

func TestWindowsDNSProjectionUpdateDoesNotRecreateInterface(t *testing.T) {
	original := wireGuardEngineRouterConfig{
		Interface: "EndlessNet", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.1/32")},
		Routes:    []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")},
		DNSProxy:  &DNSProxyOptions{ListenAddr: "127.0.0.1:53", SearchDomain: "scenario.endlessnet"},
	}
	next := cloneWireGuardEngineRouterConfig(original)
	next.DNSProxy.NetworkMap.Revision.Network = 2
	var scripts []string
	r := &windowsWireGuardEngineRouter{interfaceName: original.Interface, configured: true, current: original, runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		scripts = append(scripts, args[len(args)-1])
		return nil, nil
	}}
	if err := r.Configure(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 0 || !wireGuardEngineRouterConfigsEqual(r.current, original) {
		t.Fatal("DNS projection update disturbed Windows interface state")
	}
}

func TestWindowsFailedRouteUpdateRequiresFullRestore(t *testing.T) {
	original := wireGuardEngineRouterConfig{Interface: "EndlessNet", MTU: 1280,
		Addresses: []netip.Prefix{netip.MustParsePrefix("198.18.94.1/32")}}
	next := cloneWireGuardEngineRouterConfig(original)
	next.Routes = []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")}
	var scripts []string
	r := &windowsWireGuardEngineRouter{interfaceName: original.Interface, configured: true, current: original, runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		scripts = append(scripts, args[len(args)-1])
		if len(scripts) == 1 {
			return nil, errors.New("injected partial route update failure")
		}
		return nil, nil
	}}
	if err := r.Configure(t.Context(), next); err == nil || r.configured {
		t.Fatal("failed route update was reported as configured")
	}
	if err := r.Configure(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 3 || !strings.Contains(scripts[1], "Remove-NetIPAddress") || !strings.Contains(scripts[2], "New-NetIPAddress") || !r.configured || !wireGuardEngineRouterConfigsEqual(r.current, original) {
		t.Fatal("partial route failure did not allow full restoration of prior configuration")
	}
}
