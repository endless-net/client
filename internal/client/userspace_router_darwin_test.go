package client

import (
	"net/netip"
	"strings"
	"testing"
)

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
