package client

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const routeObservationLimit = 32
const routeOutputLimit = 16 * 1024

// ObserveOSRoutes performs a bounded, read-only sample. It does not enumerate
// the full routing table and must never be presented as complete diagnostics.
func ObserveOSRoutes(ctx context.Context, iface string, targets []string) []WireGuardRouteInspection {
	return observePlatformOSRoutes(ctx, iface, targets)
}

func observeOSRoutes(ctx context.Context, goos, iface string, targets []string, runner CommandRunner) []WireGuardRouteInspection {
	return observeRouteTargets(ctx, iface, targets, func(ctx context.Context, addr netip.Addr) (string, error) {
		name, args := routeObservationCommand(goos, addr.String())
		if name == "" {
			return "", errors.New("OS route observation unsupported")
		}
		out, err := runner(ctx, name, args...)
		if err != nil || len(out) > routeOutputLimit {
			return "", errors.New("OS route observation unavailable")
		}
		return routeObservationInterface(goos, addr, string(out)), nil
	})
}

func observeRouteTargets(ctx context.Context, iface string, targets []string, lookup func(context.Context, netip.Addr) (string, error)) []WireGuardRouteInspection {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var result []WireGuardRouteInspection
	seen := map[string]bool{}
	for _, raw := range targets {
		if ctx.Err() != nil || len(result) == routeObservationLimit {
			break
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil || addr.Zone() != "" {
			continue // Never interpolate unvalidated text into an OS command.
		}
		target := addr.String()
		if seen[target] {
			continue
		}
		seen[target] = true
		route := WireGuardRouteInspection{Target: target}
		alias, err := lookup(ctx, addr)
		if err != nil || ctx.Err() != nil {
			route.Error = "OS route observation unavailable"
		} else if alias == "" || len(alias) > 256 || strings.ContainsAny(alias, "\r\n\x00") {
			route.Error = "OS route observation invalid"
		} else {
			route.Interface = alias
			route.UsesInterface = iface != "" && alias == iface
		}
		result = append(result, route)
	}
	return result
}

func routeObservationCommand(goos, target string) (string, []string) {
	switch goos {
	case "linux":
		addr, err := netip.ParseAddr(target)
		if err != nil {
			return "", nil
		}
		family := "-6"
		if addr.Is4() {
			family = "-4"
		}
		return "ip", []string{"-j", family, "route", "get", target}
	case "darwin":
		return "route", []string{"-n", "get", target}
	default:
		return "", nil
	}
}

func routeObservationInterface(goos string, target netip.Addr, output string) string {
	switch goos {
	case "linux":
		// A stray dev token in warnings or a different destination must never
		// become an observed route. The shared JSON parser rejects duplicates.
		value, err := underlayDNSJSON([]byte(output))
		if err != nil {
			return ""
		}
		rows, ok := value.([]any)
		if !ok || len(rows) != 1 {
			return ""
		}
		row, ok := rows[0].(map[string]any)
		if !ok {
			return ""
		}
		dst, _ := row["dst"].(string)
		address, err := netip.ParseAddr(dst)
		if err != nil || address != target {
			return ""
		}
		if kind, exists := row["type"]; exists && kind != "unicast" && kind != "local" {
			return ""
		}
		if _, failed := row["error"]; failed {
			return ""
		}
		dev, _ := row["dev"].(string)
		return dev
	case "darwin":
		for _, line := range strings.Split(output, "\n") {
			if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && key == "interface" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

type routeObservationOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *routeObservationOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > routeOutputLimit-len(b.data) {
		b.overflow = true
		return 0, errors.New("route output limit exceeded")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func runRouteObservation(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideRouteObservationWindow(cmd)
	cmd.WaitDelay = 200 * time.Millisecond
	var output routeObservationOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if err != nil || output.overflow {
		return nil, errors.New("route command failed")
	}
	return output.data, nil
}
