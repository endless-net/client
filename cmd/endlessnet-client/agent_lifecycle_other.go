//go:build !linux

package main

import (
	"context"

	"github.com/endless-net/client/internal/client"
)

func runAgentPlatformLifecycle(ctx context.Context, run func(context.Context, <-chan client.RuntimeLifecycleNotification) error) error {
	return run(ctx, nil)
}
