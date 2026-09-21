package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// The same lock is used by the existing agent reconciliation loop. Holding it
// across the complete switch prevents that loop from reapplying the old map
// between Stop and target activation.
func agentRPCProfileDriver(opts agentIPCOptions) client.ClientRPCProfileDriver {
	return client.ClientRPCProfileDriver{Lock: opts.OperationMu,
		Logout: agentRPCLogout,
		Stop: func(ctx context.Context) (ipc.ConnectionContinuity, error) {
			if opts.WireGuard == nil {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			inspection := opts.WireGuard.Inspection()
			result, err := opts.WireGuard.Down(ctx)
			if err != nil || !result.OK {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
			}
			invalidateAgentSnapshot(opts)
			notifyRPCNodeOffline(ctx, opts)
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
			if err := nativeApprovalFailure(cfg.NodeApprovalState); err != nil {
				return err
			}
			if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.PrivateKey) == "" ||
				strings.TrimSpace(cfg.NodeCredential) == "" || cfg.CachedMap == nil {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
			}
			if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
				return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
			}
			networkMap, err := verifiedCachedNetworkMap(&cfg)
			if err != nil {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
			}
			if err := nativeApprovalFailure(networkMap.Node.ApprovalState); err != nil {
				return err
			}
			if cfg.ExitSelection != nil {
				if opts.ExitRuntime == nil {
					return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
				}
				var err error
				if cfg.RPCState != nil && cfg.RPCState.NetworkPreferenceChange != nil {
					err = opts.ExitRuntime.ApplyPreferenceCandidateLocked(ctx, opts.OperationMu, cfg)
				} else {
					err = opts.ExitRuntime.ResumeSavedLocked(ctx, opts.OperationMu, cfg)
				}
				if err != nil {
					return err
				}
			} else {
				result, err := opts.WireGuard.Configure(ctx, cfg, networkMap)
				if err != nil || !result.OK {
					return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
				}
			}
			invalidateAgentSnapshot(opts)
			requestAgentSync(opts)
			return nil
		},
	}
}

func nativeApprovalFailure(state string) error {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case api.NodeApprovalPending:
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_APPROVAL_REQUIRED)
	case api.NodeApprovalRejected:
		return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED)
	default:
		return nil
	}
}

// Local teardown is complete before this best-effort notification. Never let an
// unreachable control plane prevent Disconnect, and never persist the response's
// map over concurrently accepted mutations. The next sync verifies a fresh map.
func notifyRPCNodeOffline(ctx context.Context, opts agentIPCOptions) {
	if ctx.Err() != nil || opts.ConfigStore == nil || opts.WireGuard == nil {
		return
	}
	cfg := opts.ConfigStore.Read()
	if cfg.NodeID == "" || cfg.NodeCredential == "" || len(cfg.ControlURLs()) == 0 || client.ValidateConfigCurrentDevice(cfg) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	owned, err := opts.WireGuard.ControlPlaneHTTPClient(cfg)
	if owned != nil && owned.Transport != nil {
		defer owned.CloseIdleConnections()
	}
	if err != nil || owned == nil || owned.Transport == nil || ctx.Err() != nil {
		return
	}
	control := apiFromConfig(cfg)
	transportClient := *owned
	transportClient.Jar = nil
	transportClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if transportClient.Timeout <= 0 || transportClient.Timeout > 2*time.Second {
		transportClient.Timeout = 2 * time.Second
	}
	transportClient.Transport = enrollmentContextTransport{lifetime: ctx, base: owned.Transport}
	control.HTTPClient = &transportClient
	// The response is deliberately not adopted: this is notification, not sync.
	_, _ = control.UpdateNodeEndpointState(cfg.NodeID, api.UpdateNodeEndpointRequest{Status: api.NodeStatusOffline, ClientVersion: version})
}
