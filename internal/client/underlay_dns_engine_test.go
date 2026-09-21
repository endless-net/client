package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func testUnderlayDNSCapture(context.Context, string) (*underlayDNSSource, error) {
	return &underlayDNSSource{Owner: ":1.42", Links: []underlayDNSLink{{Index: 2, Name: "eth0", Servers: []netip.AddrPort{netip.MustParseAddrPort("192.0.2.53:53")}, DefaultRoute: true, DNSSEC: "no", DNSOverTLS: "no"}}, Interfaces: []underlayDNSInterface{{Index: 2, Name: "eth0", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}}}, nil
}

func TestExitDNSRecoveryRequiresContainmentAndFreshSource(t *testing.T) {
	cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
	contained := false
	guard, err := newLinuxExitGuard("endlessnet", 51820, exitGuardReadbackRunner(t, "endlessnet", 51820, func(context.Context, string, string, ...string) ([]byte, error) { contained = true; return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	source, _ := testUnderlayDNSCapture(t.Context(), "endlessnet")
	fail := true
	e := &WireGuardEngine{opts: WireGuardEngineOptions{underlayDNSCapture: func(ctx context.Context, name string) (*underlayDNSSource, error) {
		if !contained || name != "endlessnet" {
			t.Fatal("source read preceded owned containment")
		}
		if fail {
			return nil, errors.New("unavailable")
		}
		return source, ctx.Err()
	}}}
	if err := e.restoreExitUnderlay(t.Context(), cfg, guard); err == nil || e.exitGuard != guard || e.exitConfig.NodeID != "" {
		t.Fatal("failed source acquired authority or lost protection", err)
	}
	fail = false
	if err := e.restoreExitUnderlay(t.Context(), cfg, guard); err != nil {
		t.Fatal(err)
	}
	current := e.underlayDNSCurrentLocked()
	if current == nil || current(t.Context()) != nil {
		t.Fatal("fresh source was not bound")
	}
	identity := underlayDNSSourceIdentity(e.underlayDNS)
	source.Links[0].Servers[0] = netip.MustParseAddrPort("192.0.2.54:53")
	if underlayDNSSourceIdentity(e.underlayDNS) != identity || current(t.Context()) == nil {
		t.Fatal("source alias changed immutable binding or stale source accepted")
	}
	source.Links[0].Servers[0] = netip.MustParseAddrPort("192.0.2.53:53")
	source.Interfaces[0].Addresses[0] = netip.MustParseAddr("192.0.2.11")
	if underlayDNSSourceIdentity(e.underlayDNS) != identity || current(t.Context()) == nil {
		t.Fatal("unchanged DNS address hid DHCP source change")
	}
	if err := e.restoreClearProtection(t.Context(), guard); err != nil || e.underlayDNS != nil {
		t.Fatal("local clear required DNS authority", err)
	}
	fail = true
	cfg.ControlPlaneURLs = []string{"https://192.0.2.1"}
	if err := e.restoreExitUnderlay(t.Context(), cfg, guard); err != nil || e.underlayDNS != nil || e.exitConfig.NodeID != cfg.NodeID {
		t.Fatal("literal control endpoint unnecessarily required DNS", err)
	}
}
