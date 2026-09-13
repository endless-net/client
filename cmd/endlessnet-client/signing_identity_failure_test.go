package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestSigningIdentityFailureSurvivesSnapshotWithoutTextMatching(t *testing.T) {
	fixture := newRecoveryTestFixture(t, "https://control.example.test")
	cfg, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnrollmentRecovery = nil
	cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
	cfg.RPCState = &client.ClientRPCState{ActiveProfileID: "profile"}
	if err := client.SaveConfig(fixture.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/server-key" {
			t.Error("unexpected signing probe")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(testServerKeyResponseFromBundle(fixture.NewTrust))
	}))
	defer server.Close()
	changed := refreshMapSigningTrust(&cfg, api.NewAPI(server.URL, ""))
	if !errors.Is(changed, errServerMapSigningTrustChanged) {
		t.Fatal("untrusted announcement lost typed failure", changed)
	}
	for _, typed := range []bool{true, false} {
		failure := errors.New(serverMapSigningTrustChangedError)
		if typed {
			failure = fmt.Errorf("wrapped: %w", changed)
		}
		path := filepath.Join(t.TempDir(), "state.json")
		if err := writeAgentFailureSnapshot(path, fixture.ConfigPath, failure); err != nil {
			t.Fatal(err)
		}
		snapshot, err := client.LoadAgentSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		if (snapshot.FailureKind == client.AgentFailureServerIdentityChanged) != typed {
			t.Fatal("diagnostic text determined failure classification")
		}
		for _, binding := range []string{"current", "previous", "other-profile"} {
			observation := snapshot
			if binding == "previous" {
				observation.MapRevision--
			}
			if binding == "other-profile" {
				observation.ProfileID = "other"
			}
			status := &ipc.Status{ActiveProfileId: "profile", NodeId: cfg.NodeID, Network: &ipc.Network{Id: cfg.NetworkID}, MapRevision: cfg.MapRevision}
			attachAgentRPCSnapshot(status, observation)
			wantChanged := typed && binding == "current"
			if (status.ServiceState == ipc.ServiceState_SERVICE_STATE_SERVER_IDENTITY_CHANGED) != wantChanged {
				t.Fatal("identity failure escaped snapshot binding", typed, binding)
			}
			if wantChanged && (status.ControlState != ipc.ControlState_CONTROL_STATE_SERVER_IDENTITY_CHANGED || status.GetRecovery().GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_SERVER_IDENTITY_CHANGED || status.GetAgent().GetLastFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_SERVER_IDENTITY_CHANGED) {
				t.Fatal("native identity failure lost typed projection")
			}
		}
	}
}
