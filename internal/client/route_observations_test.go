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
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, mode := range []string{"ok", "missing", "error", "oversized"} {
			t.Run(goos+"/"+mode, func(t *testing.T) {
				calls := 0
				runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
					calls++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 3*time.Second || !strings.Contains(strings.Join(args, " "), "2001:db8::1") {
						t.Fatal("missing bound or literal target")
					}
					wantCommand := map[string]string{"linux": "ip", "darwin": "route", "windows": "powershell.exe"}[goos]
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
					return []byte(map[string]string{"linux": "2001:db8::1 dev tun0 src 2001:db8::2", "darwin": "route to: 2001:db8::1\n interface: tun0\n", "windows": "tun0"}[goos]), nil
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
