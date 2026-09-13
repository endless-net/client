package tests

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

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
