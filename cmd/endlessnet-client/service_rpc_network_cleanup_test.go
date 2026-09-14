package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/client"
)

func TestNetworkTargetCleanupRevokesOnlyItsNode(t *testing.T) {
	for _, mode := range []string{"registered", "lost_response", "revoke_failure", "checkpoint_failure", "wrong_operation", "wrong_network", "wrong_node", "invalid_credential"} {
		t.Run(mode, func(t *testing.T) { testNetworkTargetCleanup(t, mode) })
	}
}

func testNetworkTargetCleanup(t *testing.T, mode string) {
	t.Helper()
	setInstallationStateDirForTest(t, t.TempDir())
	server, snapshot := testPendingEnrollmentServer(t, testMapSigningKey(t), "", func(response *api.RegisterNodeResponse) { response.Network.AccountID = "account" })
	defer server.Close()
	original := server.Config.Handler
	revokes, logouts := 0, 0
	checkpointedNode := false
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/logout" {
			logouts++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodDelete {
			original.ServeHTTP(w, r)
			return
		}
		revokes++
		if r.URL.Path != "/nodes/node-pending" || r.Header.Get(nodeCredentialHeader) == "" || !checkpointedNode {
			t.Error("revocation lost target scope or preceded credential checkpoint")
		}
		if mode == "revoke_failure" {
			http.Error(w, "synthetic remote failure", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	identity, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	private, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NetworkID: "net-pending", ActiveAccountID: "account", Token: "synthetic-shared-session", IdentityPrivateKey: identity, PrivateKey: private}
	input := client.ClientRPCNetworkRegistrationInput{OperationID: "82366b0c-52c9-43e9-bebb-b984445157fc", NetworkID: "net-pending", Hostname: "target-host"}
	var uncertain client.Config
	_, err = agentRPCRegisterNetworkTarget(t.Context(), cfg, input, func(next client.Config) error {
		cfg = next
		if next.PendingDirectRegistration != nil {
			encoded, err := json.Marshal(next)
			if err != nil {
				return err
			}
			return json.Unmarshal(encoded, &uncertain)
		}
		return nil
	})
	if err != nil || cfg.NodeID != "node-pending" || uncertain.PendingDirectRegistration == nil {
		t.Fatal("fixture did not retain registered and uncertain targets", err)
	}
	if mode == "lost_response" || mode == "checkpoint_failure" || mode == "wrong_operation" {
		cfg = uncertain
	} else {
		checkpointedNode = true
	}
	before := cfg
	if mode == "wrong_operation" {
		input.OperationID = "d77186c0-81c3-4f7f-88ba-37a47d56f93f"
	}
	if mode == "wrong_network" {
		input.NetworkID = "other-network"
	}
	if mode == "wrong_node" {
		cfg.NodeID = "other-node"
	}
	if mode == "invalid_credential" {
		cfg.NodeCredential = "invalid"
	}
	err = agentRPCCleanupNetworkTarget(t.Context(), cfg, input, func(next client.Config) error {
		if mode == "checkpoint_failure" && next.NodeID != "" {
			return errors.New("synthetic checkpoint failure")
		}
		cfg = next
		checkpointedNode = next.NodeID != "" && next.NodeCredential != ""
		return nil
	})
	_, registrations := snapshot()
	wantRegistrations := 1
	if mode == "lost_response" || mode == "checkpoint_failure" {
		wantRegistrations = 2
	}
	wantRevokes := 1
	if mode == "checkpoint_failure" || mode == "wrong_operation" || mode == "wrong_network" || mode == "wrong_node" || mode == "invalid_credential" {
		wantRevokes = 0
	}
	wantSuccess := mode == "registered" || mode == "lost_response"
	if (err == nil) != wantSuccess || revokes != wantRevokes || registrations != wantRegistrations || logouts != 0 || cfg.Token != before.Token {
		t.Fatalf("cleanup outcome/binding mismatch: err=%v revoke=%d register=%d logout=%d", err, revokes, registrations, logouts)
	}
	if mode == "lost_response" {
		request, _ := snapshot()
		if !reflect.DeepEqual(request, uncertain.PendingDirectRegistration.Request) {
			t.Fatal("compensation changed the uncertain signed registration request")
		}
	}
}
