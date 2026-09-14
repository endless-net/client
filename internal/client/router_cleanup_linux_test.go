package client

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestLinuxRouterCleanupRetriesFailuresWithoutReapplying(t *testing.T) {
	for _, failureKind := range []string{"route", "dns", "interface"} {
		t.Run(failureKind, func(t *testing.T) {
			cfg := wireGuardEngineRouterConfig{Interface: "endlessnet", MTU: 1280, Routes: []netip.Prefix{netip.MustParsePrefix("198.18.94.20/32")}, DNS: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
			calls := map[string]int{}
			failing := true
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				kind := "interface"
				if name == "resolvectl" {
					kind = "dns"
				} else if len(args) > 1 && args[1] == "route" {
					kind = "route"
				}
				key := name + " " + strings.Join(args, " ")
				calls[key]++
				if failing && kind == failureKind {
					return nil, errors.New("injected removal failure")
				}
				return nil, nil
			}
			r := &linuxWireGuardEngineRouter{interfaceName: cfg.Interface, configured: true, current: cfg, runner: run, interfacePresent: func(string) (bool, error) { return true, nil }}
			if err := r.Down(t.Context()); err == nil || !r.configured || r.pendingCleanup == nil {
				t.Fatal("failed cleanup lost retry state", err)
			}
			before := make(map[string]int, len(calls))
			for key, count := range calls {
				before[key] = count
			}
			if err := r.Configure(t.Context(), cfg); err == nil {
				t.Fatal("Configure bypassed pending cleanup")
			}
			for key, count := range calls {
				if count > before[key] && (strings.Contains(key, "route replace") || strings.Contains(key, " mtu ")) {
					t.Fatal("cleanup retry applied networking", key)
				}
			}
			failing = false
			if err := r.Down(t.Context()); err != nil || r.configured || r.pendingCleanup != nil {
				t.Fatal("retry did not complete cleanup", err)
			}
			for key, count := range calls {
				kind := "interface"
				if strings.HasPrefix(key, "resolvectl ") {
					kind = "dns"
				} else if strings.Contains(key, " route ") {
					kind = "route"
				}
				want := 1
				if kind == failureKind {
					want = 3
				}
				if count != want {
					t.Fatal("cleanup repeated completed step or lost failed step", key, count, want)
				}
			}
		})
	}
}
