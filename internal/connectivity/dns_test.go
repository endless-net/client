package connectivity

import (
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestSplitDNSRoutesSelectOnlyTheLongestMatchingZone(t *testing.T) {
	config := &coordinator.DNSConfig{OverrideLocalDNS: true, Nameservers: []coordinator.DNSNameserver{
		{ID: "primary", Scope: "primary", Address: "192.0.2.1"},
		{ID: "z", Scope: "split", Address: "192.0.2.2", Priority: 1, SplitDomains: []string{"internal.example"}},
		{ID: "a", Scope: "split", Address: "192.0.2.3", Priority: 1, SplitDomains: []string{"Internal.Example.", "internal.example"}},
		{ID: "low", Scope: "split", Address: "192.0.2.5", Priority: 2, SplitDomains: []string{"internal.example"}},
		{ID: "child", Scope: "split", Address: "192.0.2.4", SplitDomains: []string{"dev.internal.example"}},
	}}
	routes, err := CompileDNSRoutes(config)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"db.DEV.internal.example.":    {"192.0.2.4"},
		"dev.internal.example":        {"192.0.2.4"},
		"_ldap._tcp.internal.example": {"192.0.2.3", "192.0.2.2", "192.0.2.5"},
		"notinternal.example":         {"192.0.2.1"},
		"internal.example.foreign":    {"192.0.2.1"},
		"printer":                     {"192.0.2.1"},
	} {
		var got []string
		for _, ip := range routes.Servers(name) {
			got = append(got, ip.String())
		}
		if !slices.Equal(want, got) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	config.OverrideLocalDNS = false
	routes, err = CompileDNSRoutes(config)
	if err != nil || len(routes.Servers("external.example")) != 0 || !slices.Equal(routes.Servers("dev.internal.example"), []netip.Addr{netip.MustParseAddr("192.0.2.4")}) {
		t.Fatal("split routing without default override", err)
	}
}

func TestDNSResolverUsesUnderlyingIPv4Address(t *testing.T) {
	cfg := &coordinator.DNSConfig{OverrideLocalDNS: true, Nameservers: []coordinator.DNSNameserver{
		{ID: "default", Scope: "primary", Address: "::ffff:192.0.2.5"},
	}}
	routes, err := CompileDNSRoutes(cfg)
	if err != nil || !slices.Equal(routes.Servers("www.example"), []netip.Addr{netip.MustParseAddr("192.0.2.5")}) {
		t.Fatal("mapped upstream", routes, err)
	}
	for _, raw := range []string{"::ffff:0.0.0.0", "::ffff:224.0.0.1", "::ffff:192.0.2.5%eth0"} {
		cfg.Nameservers[0].Address = raw
		if _, err := CompileDNSRoutes(cfg); !errors.Is(err, ErrDNSAssignment) {
			t.Fatalf("invalid upstream %s: %v", raw, err)
		}
	}
}

func TestVerifierClassifiesDNSOnlyForCurrentSignedMap(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	d := &coordinator.DNSConfig{SearchDomains: []string{"internal.example"}}
	f.value.Network.DNSConfig = d
	check := func(want error) {
		t.Helper()
		m, err := v.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
		if !errors.Is(err, want) || !errors.Is(err, ErrMap) || m.Cursor().Revision.Network != 0 {
			t.Fatalf("got %v, want %v and no map", err, want)
		}
	}
	check(ErrDNSUnsupported)
	d.Nameservers = []coordinator.DNSNameserver{{ID: "server", Scope: "split", Address: "ff02::1", SplitDomains: []string{"internal.example"}}}
	check(ErrDNSAssignment)
	d.Nameservers[0].Address = "2001:db8::1"
	d.SearchDomains = []string{"invalid..example"}
	check(ErrDNSAssignment)
	f.value.Network.AccountID = "another-account"
	if _, err := v.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil); !errors.Is(err, ErrMap) || errors.Is(err, ErrDNSAssignment) || errors.Is(err, ErrDNSUnsupported) {
		t.Fatal("foreign map classified as local DNS", err)
	}
}
