package client

import (
	"context"
	"net/netip"
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
