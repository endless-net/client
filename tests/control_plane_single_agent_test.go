package tests

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
		// Preserve lexical aliases instead of letting filepath.Join clean them.
		// All paths name the existing configuration; no identity file is read.
		separator := string(filepath.Separator)
		dir := filepath.Dir(n.Config)
		paths := []string{n.Config, dir + separator + "." + separator + filepath.Base(n.Config), dir + separator + ".." + separator + filepath.Base(dir) + separator + filepath.Base(n.Config)}
		start, results := make(chan struct{}), make(chan bool, len(paths))
		for i, path := range paths {
			args := []string{"agent", "--config", path, "--wg-interface", n.Interface}
			// Distinct IPC endpoints exclude address-in-use as a false positive.
			if runtime.GOOS == "windows" {
				args = append(args, "--ipc-pipe", n.Pipe+"-duplicate-"+strconv.Itoa(i))
			} else {
				args = append(args, "--ipc-socket", n.Socket+".dup"+strconv.Itoa(i))
			}
			go func() {
				<-start
				output, err := n.Run(args...)
				var exit *exec.ExitError
				results <- errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(string(output), "agent already running for this config")
			}()
		}
		close(start)
		for range paths {
			if !<-results {
				t.Error("concurrent agent did not exit with the configuration-ownership error (output withheld)")
			}
		}
		if t.Failed() {
			t.FailNow()
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
