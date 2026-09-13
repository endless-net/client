package main

import (
	"encoding/json"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeSnapshotRequiresGlobalPolicyBinding(t *testing.T) {
	for _, global := range []uint64{0, 9, 10, 11} {
		status := &ipc.Status{ActiveProfileId: "profile", NodeId: "node", Network: &ipc.Network{Id: "network"}, MapRevision: 7, ServiceState: ipc.ServiceState_SERVICE_STATE_CONNECTED}
		snapshot := client.AgentSnapshot{ProfileID: "profile", NodeID: "node", NetworkID: "network", MapRevision: 7, MapGlobalRevision: global, PeerCount: 3, FailureKind: client.AgentFailureServerIdentityChanged}
		// The persisted field must survive serialization, not only an in-memory call.
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var restored client.AgentSnapshot
		if err := json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		attachAgentRPCSnapshot(status, restored, 10)
		if global != 10 {
			if status.Agent != nil || status.Recovery != nil || status.ServiceState != ipc.ServiceState_SERVICE_STATE_CONNECTED {
				t.Fatal("foreign global snapshot changed native state")
			}
		} else if status.GetAgent().GetPeerCount() != 3 || status.ServiceState != ipc.ServiceState_SERVICE_STATE_SERVER_IDENTITY_CHANGED {
			t.Fatal("matching global snapshot was not attached")
		}
	}
}
