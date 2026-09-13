package tests

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

func nativeCurrentAgentFailure(status *ipc.Status) bool {
	return status != nil && status.Agent != nil && status.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && status.Agent.LastFailure != nil
}

func nativeEnrollmentAbsent(status *ipc.Status) bool {
	return status != nil && status.ServiceState == ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT && status.NodeId == "" &&
		status.StoredState != nil && !status.StoredState.NodeCredentialPresent && !status.StoredState.CachedMapPresent && !status.StoredState.CachedMapValid
}

func TestNativeEnrollmentAbsenceRequiresExplicitStoredState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status *ipc.Status
		want   bool
	}{
		{"missing-status", nil, false},
		{"missing-state", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT}, false},
		{"unspecified-service", &ipc.Status{StoredState: &ipc.StoredStatePresence{}}, false},
		{"retained-node", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, NodeId: "retained", StoredState: &ipc.StoredStatePresence{}}, false},
		{"retained-credential", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, StoredState: &ipc.StoredStatePresence{NodeCredentialPresent: true}}, false},
		{"retained-cache", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, StoredState: &ipc.StoredStatePresence{CachedMapPresent: true}}, false},
		{"inconsistent-cache", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, StoredState: &ipc.StoredStatePresence{CachedMapValid: true}}, false},
		{"explicit-absence", &ipc.Status{ServiceState: ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT, StoredState: &ipc.StoredStatePresence{}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeEnrollmentAbsent(tc.status); got != tc.want {
				t.Fatalf("enrollment absence = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestNativeAgentFailureIsNotInferredFromControlHealth(t *testing.T) {
	status := &ipc.Status{ControlState: ipc.ControlState_CONTROL_STATE_DEGRADED}
	if nativeCurrentAgentFailure(status) {
		t.Fatal("control health invented an agent failure")
	}
	status.ControlState = ipc.ControlState_CONTROL_STATE_READY
	status.Agent = &ipc.AgentStatus{SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, LastFailure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE}}
	if !nativeCurrentAgentFailure(status) {
		t.Fatal("healthy control probe hid an agent failure")
	}
	status.Agent.SnapshotState = ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
	if nativeCurrentAgentFailure(status) {
		t.Fatal("old map failure treated as current")
	}
}

func nativeControlScenario(t *testing.T) (*testcontrol.Server, *testclient.Node, string) {
	t.Helper()
	requireControlScenario(t)
	s := testcontrol.NewTLS(t)
	network, token, err := s.AddNetwork("scenario", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.TrustControlTLS(s)
	n.Enroll(s, network.Name, token)
	n.Start()
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid()
	})
	return s, n, status.NodeId
}
