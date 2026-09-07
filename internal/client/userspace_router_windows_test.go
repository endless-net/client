package client

import (
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
