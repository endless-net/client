package main

import (
	"context"

	"github.com/endless-net/client/internal/client"
)

// Endpoint publication can advance the signed map after the online iteration
// already applied its snapshot. Apply that verified cached result now; merely
// assigning its revision would fabricate a current engine observation. This is
// one bounded apply, not another endpoint publication or control-plane fetch.
func applyAgentPublishedMap(ctx context.Context, opts agentIterationOptions, previous client.AgentSnapshot) (client.AgentSnapshot, bool, error) {
	cfg, err := client.LoadConfig(opts.ConfigPath)
	if err != nil {
		return client.AgentSnapshot{}, false, err
	}
	if cfg.MapRevision == previous.MapRevision && cfg.MapGlobalRevision == previous.MapGlobalRevision {
		return previous, false, nil
	}
	snapshot, err := runAgentCachedBootstrap(ctx, opts)
	return snapshot, err == nil, err
}
