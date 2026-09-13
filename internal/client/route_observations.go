package client

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const routeObservationLimit = 32
const routeOutputLimit = 16 * 1024

// ObserveOSRoutes performs a bounded, read-only sample. It does not enumerate
// the full routing table and must never be presented as complete diagnostics.
func ObserveOSRoutes(ctx context.Context, iface string, targets []string) []WireGuardRouteInspection {
	return observeOSRoutes(ctx, runtime.GOOS, iface, targets, runRouteObservation)
}

func observeOSRoutes(ctx context.Context, goos, iface string, targets []string, runner CommandRunner) []WireGuardRouteInspection {
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
		name, args := routeObservationCommand(goos, target)
		route := WireGuardRouteInspection{Target: target}
		if name == "" {
			route.Error = "OS route observation unsupported"
		} else {
			out, err := runner(ctx, name, args...)
			if err != nil || ctx.Err() != nil || len(out) > routeOutputLimit {
				route.Error = "OS route observation unavailable"
			} else {
				route.Interface = routeObservationInterface(goos, string(out))
				if route.Interface == "" || len(route.Interface) > 256 || strings.ContainsAny(route.Interface, "\r\n\x00") {
					route.Interface = ""
					route.Error = "OS route observation invalid"
				} else {
					route.UsesInterface = iface != "" && route.Interface == iface
				}
			}
		}
		result = append(result, route)
	}
	return result
}

func routeObservationCommand(goos, target string) (string, []string) {
	switch goos {
	case "linux":
		return "ip", []string{"route", "get", target}
	case "darwin":
		return "route", []string{"-n", "get", target}
	case "windows":
		// Find-NetRoute returns address and route objects. Require one unique
		// interface alias instead of formatting either object as free text.
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $aliases=@(Find-NetRoute -RemoteIPAddress '%s' | Select-Object -ExpandProperty InterfaceAlias -Unique); if ($aliases.Count -ne 1) { throw 'ambiguous route' }; [Console]::Out.Write([string]$aliases[0])", target)
		return "powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script}
	default:
		return "", nil
	}
}

func routeObservationInterface(goos, output string) string {
	switch goos {
	case "linux":
		return parseRouteInterface(output)
	case "darwin":
		for _, line := range strings.Split(output, "\n") {
			if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && key == "interface" {
				return strings.TrimSpace(value)
			}
		}
	case "windows":
		return strings.TrimSpace(output)
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
