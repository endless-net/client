package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNativeCredentialClockRequiresOwnVerifiedAuthority(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := api.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(time.Hour)
	credential, err := api.SignNodeCredential(private, "network", "node", []string{"node:map"}, expiry)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{NodeID: "node", NetworkID: "network", NodeCredential: credential, NodeCredentialSigningTrust: &trust, Token: "synthetic-user-session"}
	status := agentRPCCredentialStatus(cfg, now)
	if status.GetState() != ipc.CredentialState_CREDENTIAL_STATE_VALID || !status.ExpiresAt.AsTime().Equal(expiry) || status.WarningAt != nil || status.AutomaticRenewalSupported {
		t.Fatal("credential clock or unsupported renewal claim incorrect")
	}
	raw, err := protojson.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), credential) || strings.Contains(string(raw), cfg.Token) {
		t.Fatal("credential projection leaked authority")
	}
	for _, mode := range []string{"no-trust", "foreign-node", "foreign-network", "tampered", "expired", "absent"} {
		t.Run(mode, func(t *testing.T) {
			candidate := cfg
			at := now
			switch mode {
			case "no-trust":
				candidate.NodeCredentialSigningTrust = nil
				candidate.MapSigningTrust = &trust
			case "foreign-node":
				candidate.NodeID = "other"
			case "foreign-network":
				candidate.NetworkID = "other"
			case "tampered":
				candidate.NodeCredential = "invalid"
			case "expired":
				at = expiry
			case "absent":
				candidate.NodeCredential = ""
			}
			got := agentRPCCredentialStatus(candidate, at)
			if got.GetExpiresAt() != nil {
				t.Fatal("unverified deadline disclosed")
			}
			if mode == "no-trust" {
				if got != nil {
					t.Fatal("map trust substituted for credential trust")
				}
				return
			}
			want := ipc.CredentialState_CREDENTIAL_STATE_BLOCKED
			if mode == "absent" {
				want = ipc.CredentialState_CREDENTIAL_STATE_ABSENT
			}
			if got.GetState() != want {
				t.Fatal("wrong credential state")
			}
		})
	}
}
