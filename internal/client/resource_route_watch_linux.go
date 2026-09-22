//go:build linux

package client

import "context"

func openResourceHostRouteWatch(ctx context.Context) (exitLANChangeStream, error) {
	return openExitLANRouteWatch(ctx)
}
