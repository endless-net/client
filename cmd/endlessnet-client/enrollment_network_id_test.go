package main

import (
	"context"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestEnrollmentExactNetworkIDAndAccountBinding(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	key := testMapSigningKey(t)
	const target = "opaque-network-without-id-prefix"
	server, snapshot := testEnrollmentServer(t, key, "", 7, func(response *api.RegisterNodeResponse) {
		response.Network.ID, response.Network.AccountID, response.Node.NetworkID = target, "account-target", target
		credential, err := api.SignNodeCredential(key, target, response.Node.ID, []string{"node:map", "node:register"}, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		response.NodeCredential = credential
	})
	defer server.Close()
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, Token: "synthetic-session", ActiveAccountID: "account-target"}
	var stored client.Config
	err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{
		NetworkID: target, Hostname: "network-target", IdempotencyKey: "fde966bf-f3dd-4f2e-b432-e0b4feb149f1",
		Save: func(next client.Config) error { stored = next; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request, calls := snapshot()
	if calls != 1 || request.NetworkID != target || request.NetworkName != "" || request.AccountID != cfg.ActiveAccountID ||
		request.SessionTokenBinding != api.RegistrationSessionTokenBinding(cfg.Token) || request.NodeCredential != "" || request.JoinToken != "" {
		t.Fatal("exact network registration lost account/session binding or used old authorization")
	}
	if err := request.Validate(); err != nil {
		t.Fatal("signed target request invalid", err)
	}
	if stored.NetworkID != target || stored.CachedMap == nil || stored.CachedMap.Network.AccountID != cfg.ActiveAccountID || stored.NodeCredentialSigningTrust == nil {
		t.Fatal("exact target response was not verified and saved")
	}
}

func TestExactNetworkEnrollmentRejectsMixedContextBeforeCheckpoint(t *testing.T) {
	for _, scenario := range []string{"missing_session", "missing_account", "old_node", "old_credential", "old_map", "different_network", "join_token", "network_name", "whitespace"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := client.Config{Token: "synthetic-session", ActiveAccountID: "account-target"}
			saves := 0
			opts := clientEnrollmentOptions{NetworkID: "target", Save: func(client.Config) error { saves++; return nil }}
			switch scenario {
			case "missing_session":
				cfg.Token = ""
			case "missing_account":
				cfg.ActiveAccountID = ""
			case "old_node":
				cfg.NodeID = "old-node"
			case "old_credential":
				cfg.NodeCredential = "synthetic-old-credential"
			case "old_map":
				cfg.CachedMap = &api.RegisterNodeResponse{}
			case "different_network":
				cfg.NetworkID = "old-network"
			case "join_token":
				opts.JoinToken = "synthetic-join-token"
			case "network_name":
				opts.Network = "a-name"
			case "whitespace":
				opts.NetworkID = " target "
			}
			if err := enrollConfiguredClient(context.Background(), cfg, opts); err == nil || err.Error() != "exact network enrollment requires an isolated account-authorized target" || saves != 0 {
				t.Fatal("mixed target context reached enrollment checkpoint")
			}
		})
	}
}
