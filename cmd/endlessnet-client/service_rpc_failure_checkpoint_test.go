package main

import (
	"errors"
	"path/filepath"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestAgentObservationCheckpointsFailureInCurrentIteration(t *testing.T) {
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
	opts := agentIPCOptions{ConfigPath: fixture.ConfigPath, StateOutput: filepath.Join(t.TempDir(), "agent.json")}
	// Even headless operation without an IPC publisher must checkpoint now,
	// not wait for the next scheduled retry to expose the failure.
	publishAgentRPCObservation(t.Context(), nil, opts, ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, errors.New("synthetic current failure"))
	snapshot := loadAgentSnapshotIfAvailable(opts.StateOutput)
	if snapshot == nil || snapshot.LastError != "synthetic current failure" || snapshot.ProfileID != "profile" {
		t.Fatal("current failure not checkpointed before publication returns")
	}
	status := buildAgentRPCStatusWithProbe(t.Context(), opts, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, false)
	if status.GetAgent().GetLastFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal("same-iteration native projection missed the current failure")
	}
	publishAgentRPCObservation(t.Context(), nil, opts, ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, errServerMapSigningTrustChanged)
	snapshot = loadAgentSnapshotIfAvailable(opts.StateOutput)
	if snapshot == nil || snapshot.FailureKind != client.AgentFailureServerIdentityChanged {
		t.Fatal("typed trust failure was not checkpointed immediately")
	}
}
