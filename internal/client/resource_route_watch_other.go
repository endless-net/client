//go:build !linux

package client

import "context"

func openResourceHostRouteWatch(context.Context) (exitLANChangeStream, error) {
	return nil, errResourceHostObservation
}
