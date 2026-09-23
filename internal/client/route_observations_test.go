package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOSRouteObservationPlatformsAndFailures(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, mode := range []string{"ok", "missing", "error", "oversized"} {
			t.Run(goos+"/"+mode, func(t *testing.T) {
				calls := 0
				runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
					calls++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 3*time.Second || !strings.Contains(strings.Join(args, " "), "2001:db8::1") {
						t.Fatal("missing bound or literal target")
					}
					wantCommand := map[string]string{"linux": "ip", "darwin": "route"}[goos]
					if name != wantCommand {
						t.Fatal("wrong platform command")
					}
					switch mode {
					case "error":
						return []byte("private-output"), errors.New("private-error")
					case "missing":
						return nil, nil
					case "oversized":
						return []byte(strings.Repeat("x", routeOutputLimit+1)), nil
					}
					return []byte(map[string]string{"linux": `[{"dst":"2001:db8::1","dev":"tun0","prefsrc":"2001:db8::2"}]`, "darwin": "route to: 2001:db8::1\n interface: tun0\n"}[goos]), nil
				}
				routes := observeOSRoutes(t.Context(), goos, "tun0", []string{"bad'input", "fe80::1%eth0", "2001:db8::1", "2001:0db8::1"}, runner)
				if calls != 1 || len(routes) != 1 || routes[0].UsesInterface != (mode == "ok") || (routes[0].Error == "") != (mode == "ok") || strings.Contains(routes[0].Error, "private") {
					t.Fatalf("incorrect bounded observation: %+v", routes)
				}
			})
		}
	}
}

func TestOSRouteObservationCancellationLimitAndUnsupported(t *testing.T) {
	var targets []string
	for i := 1; i <= 40; i++ {
		targets = append(targets, fmt.Sprintf("192.0.2.%d", i))
	}
	calls := 0
	runner := func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte("dev tun0"), nil }
	if got := observeOSRoutes(t.Context(), "linux", "tun0", targets, runner); len(got) != routeObservationLimit || calls != routeObservationLimit {
		t.Fatal("unbounded target collection")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls = 0
	if got := observeOSRoutes(ctx, "linux", "tun0", targets, runner); len(got) != 0 || calls != 0 {
		t.Fatal("cancelled request executed commands")
	}
	got := observeOSRoutes(t.Context(), "unsupported", "tun0", targets[:1], runner)
	if calls != 0 || len(got) != 1 || got[0].Error == "" {
		t.Fatal("unsupported platform executed command")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	got = observeOSRoutes(ctx, "linux", "tun0", targets, func(context.Context, string, ...string) ([]byte, error) { cancel(); return []byte("dev tun0"), nil })
	if len(got) != 1 || got[0].Error == "" || got[0].UsesInterface {
		t.Fatal("cancelled lookup published success")
	}
}

func TestRouteOutputBufferIsBounded(t *testing.T) {
	var output routeObservationOutput
	if _, err := output.Write(make([]byte, routeOutputLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("overflow")); err == nil || !output.overflow || len(output.data) != routeOutputLimit {
		t.Fatal("output buffer exceeded bound")
	}
}

func TestLinuxDiagnosticRouteRequiresOneMatchingJSONResult(t *testing.T) {
	for _, scenario := range []struct {
		name, output string
		ok           bool
	}{
		{"unicast", `[{"dst":"192.0.2.1","dev":"tun0"}]`, true},
		{"local", `[{"type":"local","dst":"192.0.2.1","dev":"lo"}]`, true},
		{"gateway", `[{"dst":"192.0.2.1","gateway":"192.0.2.254","dev":"eth0"}]`, true},
		{"text", "warning dev tun0", false},
		{"wrong_target", `[{"dst":"192.0.2.2","dev":"tun0"}]`, false},
		{"subnet", `[{"dst":"192.0.2.0/24","dev":"tun0"}]`, false},
		{"multiple", `[{"dst":"192.0.2.1","dev":"tun0"},{"dst":"192.0.2.1","dev":"eth0"}]`, false},
		{"duplicate", `[{"dst":"192.0.2.1","dev":"eth0","dev":"tun0"}]`, false},
		{"blackhole", `[{"type":"blackhole","dst":"192.0.2.1","dev":"tun0"}]`, false},
		{"error", `[{"error":"private","dst":"192.0.2.1","dev":"tun0"}]`, false},
		{"trailing", `[{"dst":"192.0.2.1","dev":"tun0"}] private`, false},
		{"invalid_name", `[{"dst":"192.0.2.1","dev":"tun0\nprivate"}]`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			routes := observeOSRoutes(t.Context(), "linux", "tun0", []string{"192.0.2.1"}, func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "ip" || strings.Join(args, " ") != "-j -4 route get 192.0.2.1" {
					t.Fatal("route query not explicitly JSON/family bound", name, args)
				}
				return []byte(scenario.output), nil
			})
			if len(routes) != 1 || (routes[0].Error == "") != scenario.ok {
				t.Fatal("incorrect diagnostic route evidence", routes)
			}
			if !scenario.ok && (routes[0].UsesInterface || routes[0].Interface != "" || strings.Contains(routes[0].Error, "private")) {
				t.Fatal("failed route leaked output or confirmed interface")
			}
		})
	}
}

func TestDarwinDiagnosticRouteRequiresExactUnambiguousDestination(t *testing.T) {
	for _, scenario := range []struct {
		name, output string
		ok           bool
	}{
		{"exact", "route to: 2001:db8::1\ninterface: utun0\n", true},
		{"wrong_destination", "route to: 2001:db8::2\ninterface: utun0\n", false},
		{"missing_destination", "interface: utun0\n", false},
		{"missing_interface", "route to: 2001:db8::1\n", false},
		{"duplicate_destination", "route to: 2001:db8::1\nroute to: 2001:db8::1\ninterface: utun0\n", false},
		{"duplicate_interface", "route to: 2001:db8::1\ninterface: en0\ninterface: utun0\n", false},
		{"error_text", "route: writing to routing socket: not in table\ninterface: utun0\n", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			routes := observeOSRoutes(t.Context(), "darwin", "utun0", []string{"2001:db8::1"}, func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "route" || strings.Join(args, " ") != "-n get 2001:db8::1" {
					t.Fatal("route lookup changed", name, args)
				}
				return []byte(scenario.output), nil
			})
			if len(routes) != 1 || (routes[0].Error == "") != scenario.ok || routes[0].UsesInterface != scenario.ok {
				t.Fatal("incorrect Darwin route evidence", routes)
			}
		})
	}
}
