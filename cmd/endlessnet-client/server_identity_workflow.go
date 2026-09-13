package main

import (
	"context"
	"fmt"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func agentRPCServerIdentity(ctx context.Context, cfg client.Config) (clientapi.SigningTrustBundle, error) {
	_, announced, err := inspectConfiguredServerIdentity(ctx, cfg)
	return announced, err
}

// inspectConfiguredServerIdentity reads validated local and announced public
// trust without changing registration or accepting the announced authority.
// It deliberately has no dependency on the retired IPC response schema.
func inspectConfiguredServerIdentity(ctx context.Context, cfg client.Config) (clientapi.SigningTrustBundle, clientapi.SigningTrustBundle, error) {
	if err := ctx.Err(); err != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, err
	}
	if err := validateMapSigningEnrollmentURLs(cfg); err != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, err
	}
	trusted, err := client.SigningTrustBundle(cfg)
	if err != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, err
	}
	// Server-key discovery is public and must not carry the current credentials.
	api := apiFromConfig(client.Config{ControlPlaneURLs: cfg.ControlURLs()})
	api.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: api.HTTPClient.Transport}
	serverKey, err := api.ServerKey()
	if ctx.Err() != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, ctx.Err()
	}
	if err != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, fmt.Errorf("fetch server signing trust bundle: %w", err)
	}
	announced := serverKey.TrustBundle
	if err := announced.Validate(); err != nil {
		return clientapi.SigningTrustBundle{}, clientapi.SigningTrustBundle{}, fmt.Errorf("invalid server signing trust bundle: %w", err)
	}
	return trusted, announced, nil
}
