package main

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func agentRPCRegisterNetworkTarget(ctx context.Context, cfg client.Config, input client.ClientRPCNetworkRegistrationInput, save func(client.Config) error) (*ipc.UserAction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if save == nil || input.OperationID == "" || input.NetworkID == "" || cfg.NetworkID != input.NetworkID ||
		cfg.ActiveAccountID == "" || cfg.Token == "" || cfg.PrivateKey == "" || cfg.IdentityPrivateKey == "" {
		return nil, errors.New("network registration requires a prepared authorized target")
	}
	if cfg.NodeID != "" {
		action, err := refreshRegisteredNetworkTarget(ctx, cfg, save)
		return action, networkTargetProviderError(ctx, err)
	}
	hostname, tags := input.Hostname, input.Tags
	if pending := cfg.PendingDirectRegistration; pending != nil {
		hostname, tags = pending.Request.Hostname, pending.Request.Tags
	}
	var action *ipc.UserAction
	var retryableTransport bool
	err := enrollConfiguredClient(ctx, cfg, clientEnrollmentOptions{
		RequestOutcome: func(status int, err error) {
			retryableTransport = retryableEnrollmentHTTPStatus(status) || retryableRPCEnrollmentError(err)
		},
		Save: save, NetworkID: input.NetworkID, IdempotencyKey: input.OperationID, Hostname: hostname, HostnameExplicit: true, Tags: tags,
		Report: func(response api.RegisterNodeResponse) {
			if response.Node.ApprovalState == api.NodeApprovalPending {
				action = &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL, ReasonKey: "node_approval_pending"}
			}
		},
	})
	if err != nil && (retryableTransport || retryableRPCEnrollmentError(err)) {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	return action, err
}

func networkTargetProviderError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var cleanup remoteCleanupError
	if (errors.As(err, &cleanup) && cleanup.Retryable) || retryableRPCEnrollmentError(err) {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	return err
}

func refreshRegisteredNetworkTarget(ctx context.Context, cfg client.Config, save func(client.Config) error) (action *ipc.UserAction, result error) {
	var retryableTransport bool
	defer func() {
		// The pinned producer aggregates failover errors as text. Capture the
		// last transport outcome instead of parsing or exposing that text.
		if result != nil && ctx.Err() == nil && retryableTransport {
			result = rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
	}()
	if cfg.NodeCredential == "" || cfg.NodeCredentialSigningTrust == nil {
		return nil, errors.New("network target credential authority unavailable")
	}
	claims, err := api.VerifyNodeCredentialWithTrustBundle(cfg.NodeCredential, *cfg.NodeCredentialSigningTrust, "node:map", time.Now())
	if err != nil || claims.NodeID != cfg.NodeID || claims.NetworkID != cfg.NetworkID {
		return nil, errors.New("network target credential rejected")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return nil, err
	}
	control := apiFromConfig(cfg)
	control.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: control.HTTPClient.Transport, outcome: func(status int, err error) {
		retryableTransport = retryableEnrollmentHTTPStatus(status) || retryableRPCEnrollmentError(err)
	}}
	if err := refreshMapSigningTrust(&cfg, control); err != nil {
		return nil, err
	}
	event, err := control.ReadMapStreamEvent(cfg.NodeID, api.MapCursor{}, 5*time.Second)
	if err != nil {
		if errors.Is(err, api.ErrMapStreamNoEvent) && cfg.NodeApprovalState == api.NodeApprovalPending {
			return &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL, ReasonKey: "node_approval_pending"}, nil
		}
		return nil, err
	}
	if event.Snapshot == nil {
		return nil, errors.New("network target requires a full snapshot")
	}
	state, _, err := cacheNetworkMapFromEvent(&cfg, event)
	if err != nil {
		return nil, err
	}
	if state.Network.AccountID != cfg.ActiveAccountID {
		return nil, errors.New("network target account mismatch")
	}
	if state.Node.ApprovalState == api.NodeApprovalRejected {
		return nil, rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := save(cfg); err != nil {
		return nil, err
	}
	if state.Node.ApprovalState == api.NodeApprovalPending {
		return &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL, ReasonKey: "node_approval_pending"}, nil
	}
	return nil, nil
}
