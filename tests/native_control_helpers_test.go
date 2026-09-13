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
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("scenario", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, token)
	n.Start()
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid()
	})
	return s, n, status.NodeId
}
