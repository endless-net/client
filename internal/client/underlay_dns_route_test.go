package client

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func underlayDNSRouteFixture(global bool) (*underlayDNSSource, underlayDNSLink, netip.AddrPort) {
	server := netip.MustParseAddrPort("192.0.2.53:53")
	link := underlayDNSLink{Index: 2, Name: "eth0", Servers: []netip.AddrPort{server}, DNSSEC: "no", DNSOverTLS: "no"}
	if global {
		link.Index = 0
		link.Name = ""
	}
	source := &underlayDNSSource{Owner: ":1.7", Links: []underlayDNSLink{link}, Interfaces: []underlayDNSInterface{{Index: 2, Name: "eth0", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}}}
	return source, link, server
}

func TestUnderlayDNSRouteObservesMarkedBoundAndGlobalSources(t *testing.T) {
	for _, global := range []bool{false, true} {
		source, link, server := underlayDNSRouteFixture(global)
		got, err := observeUnderlayDNSRoute(context.Background(), "en0", 51999, "udp", server, link, source, func(ctx context.Context, name string, args ...string) ([]byte, error) {
			want := "-j -N -4 route get 192.0.2.53 mark 51999 ipproto udp dport 53"
			if !global {
				want += " oif eth0"
			}
			if name != "ip" || strings.Join(args, " ") != want {
				t.Fatalf("command %s %v", name, args)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("missing observation deadline")
			}
			return []byte(`[{"dst":"192.0.2.53","gateway":"192.0.2.1","dev":"eth0","prefsrc":"192.0.2.10","mark":51999,"uid":0,"flags":[],"cache":[]}]`), nil
		})
		if err != nil || got.InterfaceIndex != 2 || got.InterfaceName != "eth0" || got.Source.String() != "192.0.2.10" || got.Server != server || got.Mark != 51999 {
			t.Fatalf("global %v: %+v, %v", global, got, err)
		}
	}
}

func TestUnderlayDNSRouteRejectsUnconfirmedNativeEvidence(t *testing.T) {
	valid := `[{"dst":"192.0.2.53","dev":"eth0","prefsrc":"192.0.2.10","mark":51999,"flags":[]}]`
	for _, raw := range []string{
		`[]`, `[{},{}]`, valid + `[]`, strings.Replace(valid, `"dev":"eth0"`, `"dev":"en0"`, 1), strings.Replace(valid, `"dev":"eth0"`, `"dev":"eth1"`, 1),
		strings.Replace(valid, "192.0.2.10", "192.0.2.11", 1), strings.Replace(valid, "192.0.2.53", "192.0.2.54", 1), strings.Replace(valid, "51999", "1", 1),
		strings.Replace(valid, `"flags":[]`, `"flags":["linkdown"]`, 1), strings.Replace(valid, `"flags":[]`, `"type":"local"`, 1),
		strings.Replace(valid, `"flags":[]`, `"nexthops":[]`, 1), strings.Replace(valid, `"flags":[]`, `"encap":{}`, 1), strings.Replace(valid, `"flags":[]`, `"error":101`, 1),
		strings.Replace(valid, `"dev":"eth0"`, `"dev":"en0","dev":"eth0"`, 1), strings.Repeat(" ", 1<<20) + valid,
	} {
		source, link, server := underlayDNSRouteFixture(true)
		_, err := observeUnderlayDNSRoute(context.Background(), "en0", 51999, "udp", server, link, source, func(context.Context, string, ...string) ([]byte, error) { return []byte(raw), nil })
		if !errors.Is(err, errUnderlayDNSRoute) {
			t.Fatalf("accepted malformed/unsafe evidence, error %v", err)
		}
	}
}

func TestUnderlayDNSRouteIPv6LinkLocalAndSourceIdentity(t *testing.T) {
	source, link, _ := underlayDNSRouteFixture(false)
	server := netip.MustParseAddrPort("[fe80::53%eth0]:5353")
	link.Servers = []netip.AddrPort{server}
	source.Links = []underlayDNSLink{link}
	source.Interfaces[0].Addresses = []netip.Addr{netip.MustParseAddr("fe80::10")}
	got, err := observeUnderlayDNSRoute(context.Background(), "en0", 51999, "tcp6", server, link, source, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "-j -N -6 route get fe80::53 mark 51999 ipproto tcp dport 5353 oif eth0" {
			t.Fatalf("command %v", args)
		}
		return []byte(`[{"dst":"fe80::53","dev":"eth0","from":"::","prefsrc":"fe80::10","mark":"0xcb1f","metric":1024,"pref":"medium","flags":[]}]`), nil
	})
	if err != nil || got.Server != server {
		t.Fatalf("IPv6 = %+v, %v", got, err)
	}
	source.Interfaces[0].Index = 3
	_, err = observeUnderlayDNSRoute(context.Background(), "en0", 51999, "udp", server, link, source, func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`[{"dst":"fe80::53","dev":"eth0","prefsrc":"fe80::10"}]`), nil
	})
	if err == nil {
		t.Fatal("accepted reused interface name with changed index")
	}
}

func TestUnderlayDNSRouteCancellationAndRedaction(t *testing.T) {
	source, link, server := underlayDNSRouteFixture(true)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := observeUnderlayDNSRoute(ctx, "en0", 51999, "udp", server, link, source, func(context.Context, string, ...string) ([]byte, error) {
		cancel()
		return nil, errors.New("private native output")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	_, err = observeUnderlayDNSRoute(context.Background(), "en0", 51999, "udp", server, link, source, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("private native output")
	})
	if !errors.Is(err, errUnderlayDNSRoute) || strings.Contains(err.Error(), "private") {
		t.Fatalf("command error = %v", err)
	}
}
