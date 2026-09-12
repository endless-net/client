package main

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// The same lock is used by the existing agent reconciliation loop. Holding it
// across the complete switch prevents that loop from reapplying the old map
// between Stop and target activation.
func agentRPCProfileDriver(opts agentIPCOptions) client.ClientRPCProfileDriver {
	return client.ClientRPCProfileDriver{Lock: opts.OperationMu,
		Stop: func(ctx context.Context) (ipc.ConnectionContinuity, error) {
			if opts.WireGuard == nil {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			inspection := opts.WireGuard.Inspection()
			result, err := opts.WireGuard.Down(ctx)
			if err != nil || !result.OK {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
			}
			continuity := ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
			if inspection.OK {
				continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
			}
			return continuity, nil
		},
		Start: func(ctx context.Context, cfg client.Config) error {
			if opts.WireGuard == nil {
				return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			if cfg.NodeID == "" || cfg.CachedMap == nil {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
			}
			if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
				return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
			}
			networkMap, err := verifiedCachedNetworkMap(&cfg)
			if err != nil {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
			}
			result, err := opts.WireGuard.Configure(ctx, cfg, networkMap)
			if err != nil || !result.OK {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
			}
			return nil
		},
	}
}
