package main

import (
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Never substitute map-signing keys or user-session deadlines. Older persisted
// state without credential trust cannot yield an authoritative credential clock.
func agentRPCCredentialStatus(cfg client.Config, now time.Time) *ipc.CredentialStatus {
	if cfg.NodeCredential == "" {
		return &ipc.CredentialStatus{State: ipc.CredentialState_CREDENTIAL_STATE_ABSENT}
	}
	if cfg.NodeCredentialSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" {
		return nil
	}
	claims, err := api.VerifyNodeCredentialWithTrustBundle(cfg.NodeCredential, *cfg.NodeCredentialSigningTrust, "node:map", now)
	if err != nil || claims.NodeID != cfg.NodeID || claims.NetworkID != cfg.NetworkID {
		// Verification does not return authenticated claims on failure. In
		// particular do not parse an expired/forged bearer to invent a deadline.
		return &ipc.CredentialStatus{State: ipc.CredentialState_CREDENTIAL_STATE_BLOCKED}
	}
	expires := timestamppb.New(claims.ExpiresAt)
	if expires.CheckValid() != nil {
		return nil
	}
	return &ipc.CredentialStatus{State: ipc.CredentialState_CREDENTIAL_STATE_VALID, ExpiresAt: expires}
}
