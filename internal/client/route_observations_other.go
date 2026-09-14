//go:build !windows

package client

import (
	"context"
	"os/exec"
	"runtime"
)

func observePlatformOSRoutes(ctx context.Context, iface string, targets []string) []WireGuardRouteInspection {
	return observeOSRoutes(ctx, runtime.GOOS, iface, targets, runRouteObservation)
}

func hideRouteObservationWindow(_ *exec.Cmd) {}
