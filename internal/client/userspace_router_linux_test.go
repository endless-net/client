package client

import (
	"net/netip"
	"slices"
	"testing"
)

func TestLinuxResolvedDomainsPreserveScopedAndSearchDomains(t *testing.T) {
	cfg := wireGuardEngineRouterConfig{
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSDomains:       []string{"corp.example", "nodes.example"},
		SearchDomains:    []string{"nodes.example"},
		DNSConfigPresent: true,
	}
	want := []string{"~corp.example", "~nodes.example", "nodes.example"}
	if got := linuxResolvedDomains(cfg); !slices.Equal(got, want) {
		t.Fatalf("resolved domains = %#v, want %#v", got, want)
	}
}

func TestLinuxResolvedDomainsUsesDefaultRouteForOverride(t *testing.T) {
	cfg := wireGuardEngineRouterConfig{
		DNS:              []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		DNSOverride:      true,
		DNSConfigPresent: true,
	}
	if got := linuxResolvedDomains(cfg); !slices.Equal(got, []string{"~."}) {
		t.Fatalf("resolved domains = %#v", got)
	}
}
