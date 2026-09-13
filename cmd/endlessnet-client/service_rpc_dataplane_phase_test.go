package main

import (
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestRPCObservedDataplanePhaseDuringControlFailure(t *testing.T) {
	for _, name := range []string{"connected", "no-profile", "disconnected-intent", "user-disconnected", "invalid-cache", "busy", "engine-down", "inspection-error", "explicit-disconnected", "explicit-connecting"} {
		t.Run(name, func(t *testing.T) {
			status := &ipc.Status{ActiveProfileId: "profile", Intent: &ipc.ConnectionIntent{DesiredState: ipc.DesiredState_DESIRED_STATE_CONNECTED},
				StoredState: &ipc.StoredStatePresence{CachedMapValid: true}, ControlState: ipc.ControlState_CONTROL_STATE_DEGRADED,
				Agent: &ipc.AgentStatus{LastFailure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE}}}
			inspection, available := client.WireGuardInspection{OK: true}, true
			switch name {
			case "no-profile":
				status.ActiveProfileId = ""
			case "disconnected-intent":
				status.Intent.DesiredState = ipc.DesiredState_DESIRED_STATE_DISCONNECTED
			case "user-disconnected":
				status.UserDisconnected = true
			case "invalid-cache":
				status.StoredState.CachedMapValid = false
			case "busy":
				available = false
			case "engine-down":
				inspection.OK = false
			case "inspection-error":
				inspection.Error = "synthetic failure"
			case "explicit-disconnected":
				status.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
			case "explicit-connecting":
				status.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTING
			}
			want := status.ConnectionPhase
			if name == "connected" {
				want = ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
			}
			if got := agentRPCObservedDataplanePhase(status, inspection, available); got != want {
				t.Fatalf("phase = %v, want %v", got, want)
			}
			if status.ControlState != ipc.ControlState_CONTROL_STATE_DEGRADED || status.Agent.LastFailure.Code != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
				t.Fatal("dataplane observation erased the control failure")
			}
		})
	}
}

type mapBoundStatusEngine struct {
	*testAgentWireGuard
	t             *testing.T
	network, node string
	revision      uint64
	available     bool
	calls         int
}

func (e *mapBoundStatusEngine) TryMapInspection(network, node string, revision uint64) (client.WireGuardInspection, bool) {
	e.calls++
	if network != e.network || node != e.node || revision != e.revision {
		e.t.Fatal("status inspection was not bound to the verified map")
	}
	return client.WireGuardInspection{OK: true}, e.available
}

func TestNativeCachedStatusUsesMapBoundLiveInspection(t *testing.T) {
	fixture := newRecoveryTestFixture(t, "https://control.example.test")
	cfg, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnrollmentRecovery = nil
	cfg.RPCState = &client.ClientRPCState{ActiveProfileID: "profile"}
	cfg.ConnectionIntent = &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected}
	cfg.CachedMap.MapSignature, err = api.SignNetworkMap(testMapSigningKey(t), *cfg.CachedMap)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
	networkMap, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		t.Fatal("test requires a verified signed cache")
	}
	engine := &mapBoundStatusEngine{testAgentWireGuard: &testAgentWireGuard{}, t: t, network: networkMap.Network.ID, node: networkMap.Node.ID, revision: networkMap.Network.Revision}
	for _, available := range []bool{false, true} {
		engine.available = available
		status := buildAgentRPCStatusWithProbe(t.Context(), agentIPCOptions{WireGuard: engine}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, false)
		if (status.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED) != available {
			t.Fatal("cached status did not distinguish live inspection from cache alone")
		}
		if status.ControlState != ipc.ControlState_CONTROL_STATE_OFFLINE_CACHE || status.Control != nil || status.ServiceState == ipc.ServiceState_SERVICE_STATE_CONNECTED {
			t.Fatal("live dataplane inspection fabricated control connectivity")
		}
		if available && status.ServiceState != ipc.ServiceState_SERVICE_STATE_DEGRADED {
			t.Fatal("offline connected dataplane was not reported degraded")
		}
	}
	if engine.calls != 2 {
		t.Fatal("status did not obtain a fresh observation")
	}
}
