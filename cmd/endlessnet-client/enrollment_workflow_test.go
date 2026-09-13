package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestTypedEnrollmentHonorsPersistenceRejection(t *testing.T) {
	for _, stage := range []string{"installation", "pending", "result"} {
		t.Run(stage, func(t *testing.T) {
			setInstallationStateDirForTest(t, filepath.Join(t.TempDir(), "installation-state"))
			server, snapshot := testPendingEnrollmentServer(t, testMapSigningKey(t), "synthetic-join-token")
			defer server.Close()
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}}
			rejected := errors.New("profile transaction rejected")
			var persistedNodeID string
			var rejectedOnce bool
			err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{
				JoinToken: "synthetic-join-token", Hostname: "typed-client", Network: defaultNetworkName,
				Save: func(updated client.Config) error {
					currentStage := "installation"
					if updated.PendingDirectRegistration != nil {
						currentStage = "pending"
					} else if updated.NodeID != "" {
						currentStage = "result"
					}
					if currentStage == stage {
						rejectedOnce = true
						return rejected
					}
					persistedNodeID = updated.NodeID
					return nil
				},
				Report: func(clientapi.RegisterNodeResponse) { t.Fatal("reported success after rejected persistence") },
			})
			if !errors.Is(err, rejected) || !rejectedOnce || persistedNodeID != "" {
				t.Fatalf("persistence rejection not honored at %s: %v", stage, err)
			}
			_, calls := snapshot()
			wantCalls := 0
			if stage == "result" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("registration calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestTypedEnrollmentRequiresPersistence(t *testing.T) {
	if err := enrollConfiguredClient(t.Context(), client.Config{}, clientEnrollmentOptions{}); err == nil {
		t.Fatal("accepted workflow without persistence")
	}
}

func TestTypedEnrollmentWorkflowPersistsVerifiedPendingIdentity(t *testing.T) {
	dir := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(dir, "installation-state"))
	path := filepath.Join(dir, "client.json")
	server, snapshot := testPendingEnrollmentServer(t, testMapSigningKey(t), "synthetic-join-token")
	defer server.Close()
	cfg, err := client.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ControlPlaneURLs = []string{server.URL}
	if err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{Save: func(updated client.Config) error { return client.SaveConfig(path, updated) }, JoinToken: "synthetic-join-token", Hostname: "typed-client", HostnameExplicit: true, Network: defaultNetworkName}); err != nil {
		t.Fatal(err)
	}
	saved, err := client.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NodeID != "node-pending" || saved.NodeApprovalState != clientapi.NodeApprovalPending || !strings.HasPrefix(saved.NodeCredential, "enc_") || saved.CachedMap != nil || saved.Token != "" {
		t.Fatal("typed workflow did not persist verified restricted enrollment")
	}
	request, calls := snapshot()
	if calls != 1 || request.Hostname != "typed-client" || request.JoinToken != "synthetic-join-token" {
		t.Fatal("typed inputs did not reach registration exactly once")
	}
}

func TestTypedEnrollmentWorkflowRejectsSubstitutedIdentity(t *testing.T) {
	dir := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(dir, "installation-state"))
	path := filepath.Join(dir, "client.json")
	server, _ := testPendingEnrollmentServer(t, testMapSigningKey(t), "synthetic-join-token", func(response *clientapi.RegisterNodeResponse) {
		response.Node.PublicKey = testWireGuardPublicKey("substituted")
	})
	defer server.Close()
	cfg, err := client.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ControlPlaneURLs = []string{server.URL}
	if err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{Save: func(updated client.Config) error { return client.SaveConfig(path, updated) }, JoinToken: "synthetic-join-token", Hostname: "typed-client", Network: defaultNetworkName}); err == nil {
		t.Fatal("substituted node identity accepted")
	}
	saved, err := client.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NodeID != "" || saved.NodeCredential != "" || saved.CachedMap != nil {
		t.Fatal("invalid identity was persisted as enrollment")
	}
}
