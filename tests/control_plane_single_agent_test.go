package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// HC-005: real processes must have exclusive ownership of one configuration.
// We never inspect the lock file, process-private state or persisted identity.
func TestControlPlaneSingleAgentOwnership(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	initial := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.GetStoredState().GetCachedMapValid() })
	if initial.GetMetadata().GetInstanceId() == "" || initial.ActiveProfileId == "" || initial.GetNetwork().GetId() == "" {
		t.Fatal("native owner snapshot omitted instance, profile or network identity")
	}
	alias := filepath.Join(filepath.Dir(n.Config), "config-alias.json")
	if err := os.Symlink(n.Config, alias); err != nil {
		t.Fatal("hosted runner could not create the configuration path alias")
	}
	for _, disconnected := range []bool{false, true} {
		if disconnected {
			runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000001")
		}
		before := n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.UserDisconnected == disconnected && v.GetStoredState().GetCachedMapValid()
		})
		// Preserve lexical aliases instead of letting filepath.Join clean them.
		// All paths name the existing configuration; no identity file is read.
		separator := string(filepath.Separator)
		dir := filepath.Dir(n.Config)
		paths := []string{n.Config, dir + separator + "." + separator + filepath.Base(n.Config), dir + separator + ".." + separator + filepath.Base(dir) + separator + filepath.Base(n.Config), alias}
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
		preserved := n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.ActiveProfileId == initial.ActiveProfileId && v.UserDisconnected == disconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
		})
		if preserved.GetMetadata().GetInstanceId() != before.GetMetadata().GetInstanceId() {
			t.Fatal("duplicate startup replaced the live native host")
		}
		if !disconnected {
			if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {}); err != nil {
				t.Fatal(err)
			}
			n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.MapRevision > before.MapRevision && v.GetStoredState().GetCachedMapValid()
			})
		}
		// After release, a new real process must acquire ownership and retain
		// enrollment and the user's current connection intent.
		if runtime.GOOS == "windows" {
			n.Stop()
		} else {
			n.StopWithSignal(syscall.SIGTERM)
		}
		originalPath := n.Config
		n.Config = alias
		n.Start()
		n.Config = originalPath
		successor := n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.ActiveProfileId == initial.ActiveProfileId && v.UserDisconnected == disconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
		})
		if successor.GetMetadata().GetInstanceId() == "" || successor.GetMetadata().GetInstanceId() == before.GetMetadata().GetInstanceId() {
			t.Fatal("successor did not expose a new native host instance")
		}
		info, err := os.Lstat(alias)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("agent startup or mutation replaced the configuration symlink")
		}
		// Kill the alias-started owner without a shutdown handler. The next
		// process uses the canonical path and the same IPC endpoint; neither
		// stale ownership nor a leftover socket may prevent recovery.
		state := "connected"
		if disconnected {
			state = "disconnected"
		}
		t.Logf("checking forced termination and successor startup: %s", state)
		n.Crash()
		n.Start()
		recovered := n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.ActiveProfileId == initial.ActiveProfileId && v.UserDisconnected == disconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
		})
		if recovered.GetMetadata().GetInstanceId() == "" || recovered.GetMetadata().GetInstanceId() == successor.GetMetadata().GetInstanceId() {
			t.Fatal("crash recovery reused the previous native host instance")
		}
		if !disconnected {
			if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {}); err != nil {
				t.Fatal(err)
			}
			n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.MapRevision > recovered.MapRevision && v.GetStoredState().GetCachedMapValid()
			})
		}
		// Race without an existing owner. A lock-specific loser and a live IPC
		// winner exclude address-in-use or two failed startups as success.
		n.Stop()
		func() {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			finished := make(chan struct{}, 2)
			defer func() {
				cancel()
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for range 2 {
					select {
					case <-finished:
					case <-timer.C:
						t.Error("startup race cleanup did not reap both processes")
						return
					}
				}
			}()
			start := make(chan struct{})
			results := make(chan bool, 2)
			for range 2 {
				go func() {
					defer func() { finished <- struct{}{} }()
					args := []string{"agent", "--config", n.Config, "--wg-interface", n.Interface, "--interval", "100ms", "--timeout", "300ms"}
					if runtime.GOOS == "windows" {
						args = append(args, "--ipc-pipe", n.Pipe)
					} else {
						args = append(args, "--ipc-socket", n.Socket)
					}
					cmd := exec.CommandContext(ctx, n.Binary, args...)
					if n.Namespace != "" {
						cmd = exec.CommandContext(ctx, "ip", append([]string{"netns", "exec", n.Namespace, n.Binary}, args...)...)
					}
					cmd.Env = append(os.Environ(), n.Environment...)
					var output bytes.Buffer
					cmd.Stdout, cmd.Stderr = &output, &output
					<-start
					err := cmd.Run()
					var exit *exec.ExitError
					results <- errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(output.String(), "agent already running for this config")
				}()
			}
			close(start)
			select {
			case rejected := <-results:
				if !rejected {
					t.Fatal("startup race did not produce a configuration-ownership rejection (output withheld)")
				}
			case <-ctx.Done():
				t.Fatal("startup race did not resolve within its deadline")
			}
			winner := n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.ActiveProfileId == initial.ActiveProfileId && v.UserDisconnected == disconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
			})
			if !disconnected {
				if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {}); err != nil {
					t.Fatal(err)
				}
				n.AwaitNativeStatus(func(v *ipc.Status) bool {
					return v.NodeId == id && v.MapRevision > winner.MapRevision && v.GetStoredState().GetCachedMapValid()
				})
			}
			select {
			case <-results:
				t.Fatal("startup race winner exited before ownership was released")
			default:
			}
			cancel()
			select {
			case <-results:
			case <-time.After(5 * time.Second):
				t.Fatal("startup race winner did not terminate")
			}
		}()
		n.Start()
		n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.ActiveProfileId == initial.ActiveProfileId && v.UserDisconnected == disconnected && v.GetIntent().GetDesiredState() == before.GetIntent().GetDesiredState() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
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
