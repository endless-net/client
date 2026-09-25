//go:build !windows

package client

import (
	"context"
	"runtime"
)

func observePlatformOSDefaultRoute(ctx context.Context) (bool, bool) {
	present, err := observeDefaultRoutes(ctx, runtime.GOOS, runRouteObservation)
	return present, err == nil
}
