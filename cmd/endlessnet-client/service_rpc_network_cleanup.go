package main

import (
	"context"
	"errors"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

// Cleanup is node-only: the target shares the source account session. Calling
// revokeConfiguredClient here would also log out the still-active source.
func agentRPCCleanupNetworkTarget(ctx context.Context, cfg client.Config, input client.ClientRPCNetworkRegistrationInput, save func(client.Config) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if save == nil || input.OperationID == "" || input.NetworkID == "" || cfg.NetworkID != input.NetworkID || cfg.ActiveAccountID == "" || cfg.RPCState != nil {
		return errors.New("network cleanup requires an isolated target")
	}
	if pending := cfg.PendingDirectRegistration; pending != nil {
		request := pending.Request
		if cfg.NodeID != "" || request.IdempotencyID != input.OperationID || request.NetworkID != input.NetworkID || request.AccountID != cfg.ActiveAccountID ||
			request.NetworkName != "" || request.JoinToken != "" || request.NodeCredential != "" || request.Validate() != nil {
			return errors.New("network cleanup registration binding rejected")
		}
		// Reuse the exact durable request through the native enrollment path;
		// never generate a new request merely to make compensation possible.
		_, err := agentRPCRegisterNetworkTarget(ctx, cfg, input, func(next client.Config) error {
			if err := save(next); err != nil {
				return err
			}
			cfg = next
			return nil
		})
		if err != nil {
			return err
		}
		if cfg.NodeID == "" || cfg.PendingDirectRegistration != nil {
			return errors.New("network cleanup registration outcome unresolved")
		}
	}
	if cfg.NodeID == "" {
		if cfg.NodeCredential != "" || cfg.CachedMap != nil {
			return errors.New("network cleanup authority incomplete")
		}
		return nil // Native enrollment checkpoints its request before sending it.
	}
	if cfg.NodeCredentialSigningTrust == nil {
		return errors.New("network cleanup credential trust unavailable")
	}
	claims, err := api.VerifyNodeCredentialWithTrustBundle(cfg.NodeCredential, *cfg.NodeCredentialSigningTrust, "node:map", time.Now())
	if err != nil || claims.NodeID != cfg.NodeID || claims.NetworkID != input.NetworkID {
		return errors.New("network cleanup credential binding rejected")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return err
	}
	control := apiFromConfig(cfg)
	control.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: control.HTTPClient.Transport}
	return revokeNode(ctx, control, cfg.NodeID)
}
