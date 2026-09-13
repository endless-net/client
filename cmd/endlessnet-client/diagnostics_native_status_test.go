package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func decodeNativeDiagnosticStatus(t *testing.T, payload map[string]any) *ipc.Status {
	t.Helper()
	raw, ok := payload["status"].(json.RawMessage)
	if !ok {
		t.Fatal("diagnostic status is not protobuf JSON")
	}
	status := new(ipc.Status)
	if err := protojson.Unmarshal(raw, status); err != nil {
		t.Fatal("diagnostic status violates native contract", err)
	}
	return status
}

func TestDiagnosticNativeStatusIsOfflineAndRejectsUnboundSnapshot(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	fixture := newRecoveryTestFixture(t, server.URL)
	cfg, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnrollmentRecovery = nil
	cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
	snapshot := &client.AgentSnapshot{NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, MapRevision: cfg.MapRevision}
	status := decodeNativeDiagnosticStatus(t, diagnosticsPayloadWithAgentState(cfg, snapshot))
	if probes.Load() != 0 || status.Control != nil || status.ControlState != ipc.ControlState_CONTROL_STATE_OFFLINE_CACHE ||
		status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || !status.GetStoredState().GetCachedMapValid() ||
		status.GetAgent().GetSnapshotState() != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT {
		t.Fatal("offline diagnostic inferred live status, probed control or accepted an unbound snapshot")
	}
}

func TestDiagnosticNativeStatusEncodingFailureIsTyped(t *testing.T) {
	status := decodeNativeDiagnosticStatus(t, diagnosticsPayloadWithAgentState(client.Config{NodeID: string([]byte{0xff})}, nil))
	if len(status.Failures) != 1 || status.Failures[0].Code != ipc.ErrorCode_ERROR_CODE_INTERNAL ||
		status.Failures[0].ReasonKey != "diagnostic_status_encoding_failed" || status.NodeId != "" {
		t.Fatal("invalid diagnostic text did not produce a typed encoding failure")
	}
}
