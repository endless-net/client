package main

import (
	"path/filepath"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

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
	if err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{ConfigPath: path, JoinToken: "synthetic-join-token", Hostname: "typed-client", HostnameExplicit: true, Network: defaultNetworkName}); err != nil {
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
	if err := enrollConfiguredClient(t.Context(), cfg, clientEnrollmentOptions{ConfigPath: path, JoinToken: "synthetic-join-token", Hostname: "typed-client", Network: defaultNetworkName}); err == nil {
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
