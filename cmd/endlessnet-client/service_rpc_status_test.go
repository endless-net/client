package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRPCStatusEphemeralEnrollmentRequiresVerifiedMap(t *testing.T) {
	for _, tampered := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "tampered"}[tampered], func(t *testing.T) {
			key := testMapSigningKey(t)
			networkMap := testNetworkMapWithRevision(t, key, "net-ephemeral", "node-ephemeral", 3)
			networkMap.Node.Ephemeral = true
			signature, err := clientapi.SignNetworkMap(key, networkMap)
			if err != nil {
				t.Fatal(err)
			}
			networkMap.MapSignature = signature
			if tampered {
				networkMap.Node.Hostname = "unverified-hostname"
			}
			status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, client.Config{
				NodeID: networkMap.Node.ID, NetworkID: networkMap.Network.ID, NodeCredential: "synthetic-credential",
				MapRevision:     networkMap.Revision.Network,
				MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, signature)), CachedMap: &networkMap,
			}, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
			if tampered {
				if status.Ephemeral || status.GetStoredState().GetCachedMapValid() || status.ControlState != ipc.ControlState_CONTROL_STATE_CACHE_INVALID {
					t.Fatal("unverified map supplied ephemeral enrollment state")
				}
			} else if !status.Ephemeral || status.NodeId != networkMap.Node.ID || !status.GetStoredState().GetCachedMapValid() ||
				status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
				t.Fatal("native status lost verified ephemeral enrollment or inferred connectivity")
			}
		})
	}
}

func TestRPCStatusEnrollmentLifecycleRejectsStaleAgentError(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "unenrolled", true: "pending approval"}[pending], func(t *testing.T) {
			cfg := client.Config{}
			serviceState := ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT
			controlState := ipc.ControlState_CONTROL_STATE_NOT_REGISTERED
			if pending {
				cfg.NodeID, cfg.NetworkID, cfg.NodeCredential = "node-pending", "net-pending", "synthetic-credential"
				cfg.NodeApprovalState, cfg.EnrollmentRequestID = clientapi.NodeApprovalPending, "approval-request"
				serviceState, controlState = ipc.ServiceState_SERVICE_STATE_NEEDS_APPROVAL, ipc.ControlState_CONTROL_STATE_PENDING_APPROVAL
			}
			status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
			attachAgentRPCSnapshot(status, client.AgentSnapshot{
				NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, LastError: "synthetic private polling failure",
			})
			if status.ServiceState != serviceState || status.ControlState != controlState || status.GetAgent().GetLastFailure() != nil ||
				status.GetAgent().GetSnapshotState() != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT {
				t.Fatal("unbound agent error replaced authoritative enrollment state")
			}
			if pending && (status.EnrollmentRequestId != "approval-request" || status.GetPendingAction().GetKind() != ipc.UserAction_KIND_WAIT_FOR_APPROVAL) {
				t.Fatal("pending native status lost its approval action")
			}
		})
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

func TestRPCStatusLoadsOnlyBoundSnapshotsWithoutReplacingMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Error("unexpected control probe path")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	cfg := client.Config{NodeID: "node-1", NetworkID: "net-1", NodeCredential: "synthetic-credential", MapRevision: 7, CachedMap: &networkMap,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		RPCState:        &client.ClientRPCState{ActiveProfileID: "profile"}}
	for _, online := range []bool{false, true} {
		cfg.ControlPlaneURLs = nil
		if online {
			cfg.ControlPlaneURLs = []string{server.URL}
		}
		for _, mode := range []string{"previous", "current", "future", "previous-enrollment", "previous-profile"} {
			snapshot := client.AgentSnapshot{ProfileID: "profile", NodeID: "node-1", NetworkID: "net-1", MapRevision: 6, PeerCount: 2,
				Paths: []client.PeerPathStatus{{SelectedPath: "direct"}, {SelectedPath: "relay"}}}
			switch mode {
			case "current":
				snapshot.MapRevision = 7
			case "future":
				snapshot.MapRevision = 8
			case "previous-enrollment":
				snapshot = client.AgentSnapshot{LastError: "synthetic private previous enrollment error"}
			case "previous-profile":
				snapshot.ProfileID = "other-profile"
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "agent-state.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			status := buildAgentRPCStatus(t.Context(), agentIPCOptions{StateOutput: path}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED)
			if status.MapRevision != 7 || status.PeerCount != uint32(len(networkMap.Peers)) || !status.GetStoredState().GetCachedMapValid() {
				t.Fatal("snapshot replaced authoritative map projection", mode)
			}
			if online && status.ControlState != ipc.ControlState_CONTROL_STATE_READY || !online && status.ServiceState != ipc.ServiceState_SERVICE_STATE_DEGRADED {
				t.Fatal("snapshot replaced control availability", mode)
			}
			if mode != "previous" && mode != "current" {
				if status.Agent.GetNodeId() != "" || status.Agent.GetLastFailure() != nil {
					t.Fatal("foreign/future snapshot was exposed", mode)
				}
				continue
			}
			wantState, wantTarget := ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, uint64(0)
			if mode == "previous" {
				wantState, wantTarget = ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS, 7
			}
			if status.Agent.GetSnapshotState() != wantState || status.Agent.MapRevision != snapshot.MapRevision || status.Agent.TargetMapRevision != wantTarget || status.Agent.PeerCount != 2 || status.Agent.DirectPathCount != 1 || status.Agent.RelayPathCount != 1 {
				t.Fatal("bound snapshot lost revision or typed path counts", mode)
			}
		}
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
