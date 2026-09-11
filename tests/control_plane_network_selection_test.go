package tests

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-020/HC-023: network-scoped enrollment is observable through CLI/IPC.
// This does not imply support for multiple saved profiles or account switching.
func TestControlPlaneNetworkSelectionBoundary(t *testing.T) {
	s, n, id := controlScenario(t)
	initial := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid })
	foreign, _, err := s.AddNetwork("foreign-network", "198.18.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	list := func() ipc.NetworksResponse {
		t.Helper()
		var response ipc.NetworksResponse
		n.Service("networks", &response)
		if response.SelectedNetworkID != initial.NetworkID || len(response.Networks) != 1 || response.Networks[0].ID != initial.NetworkID {
			t.Fatal("network list exposed an unenrolled network or lost its selected network")
		}
		return response
	}
	listed := list()
	for _, ref := range []string{initial.NetworkID, listed.Networks[0].Name, strings.ToUpper(listed.Networks[0].Name)} {
		output, err := n.ServiceCommand("select-network", "--network-id", ref)
		var response ipc.SelectNetworkResponse
		if err != nil || json.Unmarshal(output, &response) != nil {
			t.Fatal("selection of the enrolled network failed (output withheld)")
		}
		if response.NodeID != id || response.SelectedNetworkID != initial.NetworkID || response.SelectedNetwork.ID != initial.NetworkID {
			t.Fatal("selection changed the enrolled network or identity")
		}
	}
	for _, disconnected := range []bool{false, true} {
		if disconnected {
			var response ipc.DisconnectResponse
			n.Service("disconnect", &response)
		}
		before := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.UserDisconnected == disconnected })
		for _, ref := range []string{foreign.ID, foreign.Name, "absent-network"} {
			output, err := n.ServiceCommand("select-network", "--network-id", ref)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "switching networks requires a new network-scoped enrollment token") {
				t.Fatal("foreign network selection did not require enrollment (output withheld)")
			}
			list()
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == id && v.NetworkID == initial.NetworkID && v.UserDisconnected == disconnected && v.DesiredState == before.DesiredState && v.NodeCredentialPresent && v.CachedMapValid
			})
		}
		n.Stop()
		n.Start()
		list()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.NetworkID == initial.NetworkID && v.UserDisconnected == disconnected && v.DesiredState == before.DesiredState && v.CachedMapValid
		})
	}
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("network selection unexpectedly registered another identity")
	}
}
