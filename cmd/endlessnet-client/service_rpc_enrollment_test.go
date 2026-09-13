package main

import (
	"path/filepath"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestAgentRPCEnrollUsesTypedWorkflowAndPendingAction(t *testing.T) {
	setInstallationStateDirForTest(t, filepath.Join(t.TempDir(), "installation-state"))
	server, snapshot := testPendingEnrollmentServer(t, testMapSigningKey(t), "synthetic-join-token")
	defer server.Close()
	var saved client.Config
	action, err := agentRPCEnroll(t.Context(), client.Config{ControlPlaneURLs: []string{server.URL}}, client.ClientRPCEnrollmentInput{
		OperationID: "2e8e2f42-1fcf-4110-b279-4a1b999f2034", Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_SERVER,
		Hostname: "native-node", Token: "synthetic-join-token",
	}, func(cfg client.Config) error { saved = cfg; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if action.GetKind() != ipc.UserAction_KIND_WAIT_FOR_APPROVAL || saved.NodeID != "node-pending" || saved.CachedMap != nil {
		t.Fatal("native provider did not save verified restricted enrollment")
	}
	request, calls := snapshot()
	if calls != 1 || request.Hostname != "native-node" || len(request.Tags) != 1 || request.Tags[0] != "mode:server" {
		t.Fatal("native registration input lost")
	}
}
