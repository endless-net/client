package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsNativeRouteObservation(t *testing.T) {
	for _, target := range []string{"192.0.2.1", "2001:db8::1"} {
		for _, mode := range []string{"ok", "lookup-error", "zero-index", "missing", "replaced", "invalid-alias", "cancel-lookup", "cancel-interface"} {
			t.Run(target+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				addr := netip.MustParseAddr(target)
				lookups, interfaces := 0, 0
				best := func(sa windows.Sockaddr, index *uint32) error {
					lookups++
					switch v := sa.(type) {
					case *windows.SockaddrInet4:
						if !addr.Is4() || v.Addr != addr.As4() || v.Port != 0 {
							t.Fatal("wrong IPv4 target")
						}
					case *windows.SockaddrInet6:
						if !addr.Is6() || v.Addr != addr.As16() || v.Port != 0 || v.ZoneId != 0 {
							t.Fatal("wrong IPv6 target")
						}
					default:
						t.Fatal("wrong address family")
					}
					if mode == "lookup-error" {
						return errors.New("private-native-error")
					}
					if mode != "zero-index" {
						*index = 17
					}
					if mode == "cancel-lookup" {
						cancel()
					}
					return nil
				}
				byIndex := func(index int) (*net.Interface, error) {
					interfaces++
					if index != 17 {
						t.Fatal("wrong observed interface")
					}
					if mode == "missing" {
						return nil, errors.New("private-interface-error")
					}
					if mode == "cancel-interface" {
						cancel()
					}
					out := &net.Interface{Index: index, Name: "tunnel0"}
					if mode == "replaced" {
						out.Index++
					}
					if mode == "invalid-alias" {
						out.Name = "bad\nalias"
					}
					return out, nil
				}
				routes := observeRouteTargets(ctx, "tunnel0", []string{"invalid", "fe80::1%zone", target, target}, func(ctx context.Context, addr netip.Addr) (string, error) {
					return windowsRouteInterface(ctx, addr, best, byIndex)
				})
				if lookups != 1 || len(routes) != 1 {
					t.Fatal("invalid or duplicate lookup")
				}
				if routes[0].UsesInterface != (mode == "ok") || (routes[0].Error == "") != (mode == "ok") || strings.Contains(routes[0].Error, "private") {
					t.Fatal("invalid observation or private error")
				}
				if mode != "ok" && routes[0].Interface != "" {
					t.Fatal("failed lookup asserted interface")
				}
				if (mode == "lookup-error" || mode == "zero-index" || mode == "cancel-lookup") && interfaces != 0 {
					t.Fatal("failed route lookup inspected interface")
				}
			})
		}
	}
}
