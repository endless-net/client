package main

import (
	"context"
	"errors"
	"strings"

	"github.com/endless-net/client/internal/client"
)

func agentRPCLogout(ctx context.Context, cfg client.Config, progress client.ClientRPCLogoutProgress, checkpoint func(client.ClientRPCLogoutProgress) error) (string, error) {
	err := revokeConfiguredClient(ctx, cfg, progress, checkpoint)
	var remote remoteCleanupError
	if errors.As(err, &remote) {
		return remote.RequestID, err
	}
	return "", err
}

// revokeConfiguredClient confirms all applicable remote cleanup before the
// caller may delete local registration. It never writes config, removes files
// or invokes a CLI command. Native callers retain their operation context.
func revokeConfiguredClient(ctx context.Context, cfg client.Config, progress client.ClientRPCLogoutProgress, checkpoint func(client.ClientRPCLogoutProgress) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkpoint == nil || progress.SessionRevoked && !progress.NodeRevoked {
		return errors.New("valid logout progress persistence is required")
	}
	if len(cfg.ControlURLs()) == 0 || (strings.TrimSpace(cfg.Token) == "" && strings.TrimSpace(cfg.NodeCredential) == "") {
		return errors.New("remote logout requires a control origin and enrollment or session")
	}
	if strings.TrimSpace(cfg.NodeID) == "" && strings.TrimSpace(cfg.NodeCredential) != "" {
		return errors.New("node_id is required to revoke node credential")
	}
	if strings.TrimSpace(cfg.NodeCredential) != "" {
		if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
			return err
		}
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: api.HTTPClient.Transport}
	if !progress.NodeRevoked {
		if strings.TrimSpace(cfg.NodeID) != "" {
			if err := revokeNode(ctx, api, cfg.NodeID); err != nil {
				return err
			}
		}
		progress.NodeRevoked = true
		if err := checkpoint(progress); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !progress.SessionRevoked {
		if strings.TrimSpace(cfg.Token) != "" {
			if err := api.Logout(); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return remoteCleanupError{cause: err}
			}
		}
		progress.SessionRevoked = true
		if err := checkpoint(progress); err != nil {
			return err
		}
	}
	return nil
}
