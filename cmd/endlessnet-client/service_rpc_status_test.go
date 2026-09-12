package main

import (
	"context"
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
	status := &ipc.Status{NodeId: "node", Network: &ipc.Network{Id: "network"}, MapRevision: 3, Agent: &ipc.AgentStatus{}}
	for _, snapshot := range []client.AgentSnapshot{{NodeID: "other", NetworkID: "network", MapRevision: 3}, {NodeID: "node", NetworkID: "other", MapRevision: 3}, {NodeID: "node", NetworkID: "network", MapRevision: 4}} {
		attachAgentRPCSnapshot(status, snapshot)
		if status.Agent.NodeId != "" {
			t.Fatal("foreign or future snapshot was attached")
		}
	}
	attachAgentRPCSnapshot(status, client.AgentSnapshot{NodeID: "node", NetworkID: "network", MapRevision: 2, LastError: "private diagnostic"})
	if status.Agent.SnapshotState != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS || status.Agent.TargetMapRevision != 3 || status.Agent.LastFailure.ReasonKey != "agent_observation_failed" {
		t.Fatal("previous snapshot or diagnostic projection incorrect")
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
