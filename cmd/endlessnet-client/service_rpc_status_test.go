package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestRPCStatusDoesNotInferConnectionOrDeadlines(t *testing.T) {
	cfg := client.Config{NodeID: "node", ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected}}
	status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTING)
	if status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTING || status.ServiceState == ipc.ServiceState_SERVICE_STATE_CONNECTED {
		t.Fatal("enrollment/intent inferred live connection")
	}
	if status.Session != nil || status.Credential != nil {
		t.Fatal("inferred provider deadlines")
	}
	status = buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED)
	if status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED || status.ServiceState != ipc.ServiceState_SERVICE_STATE_DEGRADED {
		t.Fatal("missing control/map proof was reported healthy")
	}
}

func TestRPCStatusRejectsUnverifiedCache(t *testing.T) {
	cfg := client.Config{NodeID: "node", NetworkID: "network", CachedMap: &clientapi.RegisterNodeResponse{}}
	status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
	if status.StoredState.CachedMapValid || status.Network != nil || status.ControlState != ipc.ControlState_CONTROL_STATE_CACHE_INVALID || status.Failures[0].ReasonKey != "cached_map_invalid" {
		t.Fatal("invalid cache leaked into status")
	}
}

func TestRPCStatusAgentSnapshotMustMatchVerifiedIdentity(t *testing.T) {
	status := &ipc.Status{ActiveProfileId: "profile", NodeId: "node", Network: &ipc.Network{Id: "network"}, MapRevision: 3, Agent: &ipc.AgentStatus{}}
	for _, snapshot := range []client.AgentSnapshot{{ProfileID: "profile", NodeID: "other", NetworkID: "network", MapRevision: 3}, {ProfileID: "profile", NodeID: "node", NetworkID: "other", MapRevision: 3}, {ProfileID: "profile", NodeID: "node", NetworkID: "network", MapRevision: 4}, {ProfileID: "previous-profile", NodeID: "node", NetworkID: "network", MapRevision: 3}, {NodeID: "node", NetworkID: "network", MapRevision: 3}} {
		attachAgentRPCSnapshot(status, snapshot)
		if status.Agent.NodeId != "" {
			t.Fatal("foreign or future snapshot was attached")
		}
	}
	attachAgentRPCSnapshot(status, client.AgentSnapshot{ProfileID: "profile", NodeID: "node", NetworkID: "network", MapRevision: 2, LastError: "private diagnostic"})
	if status.Agent.SnapshotState != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS || status.Agent.TargetMapRevision != 3 || status.Agent.LastFailure.ReasonKey != "agent_observation_failed" {
		t.Fatal("previous snapshot or diagnostic projection incorrect")
	}
}

func TestRPCFailureSnapshotPersistsProfileBinding(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "client.json")
	store, err := client.OpenConfigStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *client.Config) error {
		cfg.ControlPlaneURLs = []string{"https://control.example.test"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mutations, err := client.NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := mutations.AdoptInitialProfile(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := writeAgentFailureSnapshot(path, configPath, errors.New("synthetic failure")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.LoadAgentSnapshot(path)
	if err != nil || snapshot.ProfileID == "" || snapshot.ProfileID != store.Read().RPCState.ActiveProfileID {
		t.Fatal("failure snapshot lost its profile binding", err)
	}
}

func TestRPCControlProbeDoesNotFollowRedirectsOrSendCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" || r.Header.Get("Authorization") != "" {
			t.Error("invalid readiness request")
		}
		w.Header().Set("Location", "https://untrusted.example.test/")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	probe := probeAgentRPCControl(t.Context(), []string{server.URL})
	if probe.Ok || probe.HttpStatus != 302 || probe.Failure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal("redirect probe was accepted")
	}
	probe = probeAgentRPCControl(t.Context(), []string{"https://user:secret@control.test"})
	if probe.Origin != "" || probe.Failure.Code != ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatal("credential-bearing origin was disclosed/probed")
	}
}

func TestRPCIterationPhaseRequiresApplyAndInspection(t *testing.T) {
	for _, apply := range []bool{false, true} {
		for _, inspected := range []bool{false, true} {
			for _, failed := range []bool{false, true} {
				phase := agentRPCIterationPhase(client.AgentSnapshot{Apply: &client.WireGuardApplyResult{OK: apply}, WireGuard: &client.WireGuardInspection{OK: inspected}}, failed)
				if (phase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED) != (apply && inspected && !failed) {
					t.Fatal("phase not based on live apply and inspection")
				}
			}
		}
	}
	if agentRPCIterationPhase(client.AgentSnapshot{}, false) != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED {
		t.Fatal("absent observations treated as connected")
	}
}

func TestRPCObservationUsesNativeProjection(t *testing.T) {
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := client.NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := observeAgentRPCStatus(context.Background(), mutations, agentIPCOptions{}, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED); err != nil {
		t.Fatal(err)
	}
	if mutations.Metadata().Revision != 2 {
		t.Fatal("native observation was not published")
	}
}
