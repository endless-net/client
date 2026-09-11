package tests

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-005: real processes must have exclusive ownership of one configuration.
// We never inspect the lock file, process-private state or persisted identity.
func TestControlPlaneSingleAgentOwnership(t *testing.T) {
	s, n, id := controlScenario(t)
	initial := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid })
	for _, disconnected := range []bool{false, true} {
		if disconnected {
			var response ipc.DisconnectResponse
			n.Service("disconnect", &response)
		}
		before := n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.UserDisconnected == disconnected && v.CachedMapValid
		})
		args := []string{"agent", "--config", n.Config, "--wg-interface", n.Interface}
		// A separate IPC endpoint excludes address-in-use as a false positive.
		if runtime.GOOS == "windows" {
			args = append(args, "--ipc-pipe", n.Pipe+"-duplicate")
		} else {
			args = append(args, "--ipc-socket", n.Socket+".dup")
		}
		output, err := n.Run(args...)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "agent already running for this config") {
			t.Fatal("duplicate agent did not exit with the configuration-ownership error (output withheld)")
		}
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.NetworkID == initial.NetworkID && v.UserDisconnected == disconnected && v.DesiredState == before.DesiredState && v.NodeCredentialPresent && v.CachedMapValid
		})
		if !disconnected {
			if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {}); err != nil {
				t.Fatal(err)
			}
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == id && v.MapRevision > before.MapRevision && v.CachedMapValid
			})
		}
		// After release, a new real process must acquire ownership and retain
		// enrollment and the user's current connection intent.
		n.Stop()
		n.Start()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.NetworkID == initial.NetworkID && v.UserDisconnected == disconnected && v.DesiredState == before.DesiredState && v.NodeCredentialPresent && v.CachedMapValid
		})
	}
	created := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
	}
	if created != 1 {
		t.Fatal("duplicate-agent rejection or process restart created another enrollment")
	}
}
