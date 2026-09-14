package main

import (
	"context"
	"encoding/json"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// Called under the effect lock, before ordinary workers can reapply a tunnel.
// No control probe or previous successful dataplane snapshot can establish the
// result of this transition. A failed teardown remains Disconnecting.
func observeAgentRuntimeLifecycle(ctx context.Context, mutations *client.ClientRPCMutations, opts agentIPCOptions, stopped bool, failure error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return mutations.ObserveStatus(func(cfg client.Config) (*ipc.Status, error) {
		if opts.StateOutput != "" {
			snapshot := client.AgentSnapshot{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339), NodeID: cfg.NodeID, NetworkID: cfg.NetworkID,
				MapRevision: cfg.MapRevision, MapGlobalRevision: cfg.MapGlobalRevision,
			}
			if cfg.RPCState != nil {
				snapshot.ProfileID = cfg.RPCState.ActiveProfileID
			}
			if failure != nil {
				snapshot.LastError = "runtime_lifecycle_transition_pending"
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				return nil, err
			}
			if err := client.WriteFileAtomic(opts.StateOutput, append(raw, '\n'), 0o600); err != nil {
				return nil, err
			}
		}
		phase := ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTING
		if stopped {
			phase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
		}
		status := buildAgentRPCStatusWithProbe(ctx, opts, cfg, phase, false)
		if failure != nil {
			status.Failures = append(status.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "runtime_lifecycle_transition_pending", Retryable: true})
		}
		return status, ctx.Err()
	})
}
