package main

import (
	"encoding/json"
	"net/http"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNetworkTargetApprovalRefreshDoesNotReregister(t *testing.T) {
	for _, mode := range []string{"approved", "tampered", "foreign_account", "temporary"} {
		t.Run(mode, func(t *testing.T) { testNetworkTargetApprovalRefresh(t, mode) })
	}
}

func testNetworkTargetApprovalRefresh(t *testing.T, mode string) {
	t.Helper()
	setInstallationStateDirForTest(t, t.TempDir())
	key := testMapSigningKey(t)
	var approved api.RegisterNodeResponse
	server, snapshot := testPendingEnrollmentServer(t, key, "", func(response *api.RegisterNodeResponse) { response.Network.AccountID = "account"; approved = *response })
	defer server.Close()
	original := server.Config.Handler
	streams := 0
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/maps/node-pending/stream" {
			original.ServeHTTP(w, r)
			return
		}
		streams++
		if mode == "temporary" && streams == 1 {
			writeRecoveryPublicError(t, w, api.ErrorCodeTemporarilyUnavailable, "synthetic-map-retry")
			return
		}
		if r.Header.Get("X-EndlessNet-Node-Credential") == "" {
			t.Error("approval refresh omitted node credential")
		}
		approved.Node.ApprovalState = api.NodeApprovalApproved
		if mode == "foreign_account" {
			approved.Network.AccountID = "other-account"
		}
		var err error
		approved.MapSignature, err = api.SignNetworkMap(key, approved)
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture failed", 500)
			return
		}
		setTestMapStreamResponseHeaders(w)
		event := testMapStreamSnapshotEvent(t, approved)
		if mode == "tampered" {
			event.Snapshot.Network.Name = "changed-after-signing"
		}
		_ = json.NewEncoder(w).Encode(event)
	})
	identity, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	private, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NetworkID: "net-pending", ActiveAccountID: "account", Token: "synthetic-session", IdentityPrivateKey: identity, PrivateKey: private}
	input := client.ClientRPCNetworkRegistrationInput{OperationID: "82366b0c-52c9-43e9-bebb-b984445157fc", NetworkID: "net-pending", Hostname: "target-host"}
	save := func(next client.Config) error { cfg = next; return nil }
	action, err := agentRPCRegisterNetworkTarget(t.Context(), cfg, input, save)
	if err != nil || action.GetKind() != ipc.UserAction_KIND_WAIT_FOR_APPROVAL || cfg.NodeID != "node-pending" || cfg.CachedMap != nil {
		t.Fatal("pending registration did not retain isolated authority", err)
	}
	input.Hostname = "must-not-register-again"
	action, err = agentRPCRegisterNetworkTarget(t.Context(), cfg, input, save)
	wantStreams := 1
	if mode == "temporary" {
		if failure := rpc.FailureFromError(err); failure == nil || failure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || cfg.CachedMap != nil {
			t.Fatal("temporary map response lost its retry classification or changed target")
		}
		action, err = agentRPCRegisterNetworkTarget(t.Context(), cfg, input, save)
		wantStreams = 2
	}
	if mode != "approved" && mode != "temporary" {
		_, calls := snapshot()
		if err == nil || cfg.CachedMap != nil || cfg.NodeApprovalState != api.NodeApprovalPending || calls != 1 || streams != 1 {
			t.Fatal("invalid approval snapshot changed target authority or repeated registration")
		}
		return
	}
	if err != nil || action != nil {
		t.Fatal("approval refresh failed", err)
	}
	_, calls := snapshot()
	if calls != 1 || streams != wantStreams || cfg.CachedMap == nil || cfg.CachedMap.Node.Hostname != "target-host" || cfg.NodeApprovalState != api.NodeApprovalApproved {
		t.Fatal("approval polling repeated registration or lost verified context")
	}
}
