package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"
)

func TestAgentDiagnosticsPreservesDisconnectedIntent(t *testing.T) {
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	opts := agentIPCOptions{ConfigPath: filepath.Join(t.TempDir(), "client.json"), WireGuard: &testAgentWireGuard{inspectionBusy: true}}
	if err := client.SaveConfig(opts.ConfigPath, client.Config{
		NodeID: "node-1", NetworkID: "net-1", MapRevision: 7,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:       &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	if err := agentConnectionIntentStore(opts).SetDisconnected("test"); err != nil {
		t.Fatal(err)
	}
	response, err := agentIPCHandlers(opts).Diagnostics(context.Background(), ipc.DiagnosticsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	status := response.Diagnostics.Status
	if status.State != ipc.StateDisconnected || status.DesiredState != ipc.DesiredDisconnected || !status.UserDisconnected {
		t.Fatalf("diagnostics lost disconnected intent: state=%s desired=%s disconnected=%t", status.State, status.DesiredState, status.UserDisconnected)
	}
	public, err := agentIPCStatus(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []ipc.StatusResponse{status, public} {
		if current.NodeID != "node-1" || !current.CachedMapValid || !current.UserDisconnected || current.WireGuard == nil || current.WireGuard.OK || !strings.Contains(current.WireGuard.Error, "inspection unavailable") {
			t.Fatal("busy inspection prevented status from preserving identity, intent and explicit availability")
		}
	}
}
