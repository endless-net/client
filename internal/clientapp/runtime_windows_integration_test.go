//go:build windows && integration

package clientapp

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/windowsservice"
	"golang.org/x/sys/windows/svc"
)

func TestWindowsRuntimeAssemblyPreservesStateAndAuthenticatesIPC(t *testing.T) {
	if testing.Short() {
		t.Skip("separate isolated Windows application fixture")
	}
	c := stateFixtureConfig(t)
	original := seedState(t, c)
	cipher, err := os.ReadFile(filepath.Join(c.StateDirectory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	for range 2 {
		build, digest := runtimeMetadata(t)
		r, err := NewWindows(c, build, digest, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if previous == r.PipeConfig().InstanceID {
			t.Fatal("restart retained process instance")
		}
		previous = r.PipeConfig().InstanceID
		build.Version = "changed-by-caller"
		if err := r.App.Start(budget(t)); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if err := r.App.Stop(budget(t)); err != nil {
					t.Error(err)
				}
			}()
			connection, err := localipc.Connect(budget(t), r.PipeConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			client := clientv0.NewClientServiceClient(connection)
			info, err := client.GetRuntimeInfo(budget(t), &clientv0.GetRuntimeInfoRequest{})
			if err != nil || info.Runtime.Build.Version != "fixture" || info.Runtime.InstanceId != r.PipeConfig().InstanceID {
				t.Fatal("runtime bootstrap identity changed", err)
			}
			status, err := client.GetStatus(budget(t), &clientv0.GetStatusRequest{})
			if err != nil || status.Status.ActiveProfileId != c.DefaultProfileID {
				t.Fatal("owner snapshot unavailable", err)
			}
			current, err := r.state.Store().Load(budget(t))
			if err != nil || current.Revision != original.Revision || current.OwnerSID != original.OwnerSID || current.Profiles[c.DefaultProfileID].Attempt.RequestID != original.Profiles[c.DefaultProfileID].Attempt.RequestID {
				t.Fatal("startup silently changed durable attempt", err)
			}
			if !r.workers.Healthy() {
				t.Fatal("mandatory loops not healthy")
			}
		}()
		if r.workers.Healthy() {
			t.Fatal("worker survived Stop")
		}
		if _, err := r.state.Store().Load(budget(t)); !errors.Is(err, ErrProtectedState) {
			t.Fatal("state admitted work after Stop", err)
		}
		if after, err := os.ReadFile(filepath.Join(c.StateDirectory, "state.bin")); err != nil || !bytes.Equal(after, cipher) {
			t.Fatal("read-only restart changed protected state", err)
		}
		assertPipeClosed(t, r.PipeConfig())
	}
}

func TestWindowsRuntimeRefusesUnpreparedStateAndRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows startup rollback fixture")
	}
	for _, mode := range []string{"missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			c := stateFixtureConfig(t)
			if mode == "corrupt" {
				if err := os.WriteFile(filepath.Join(c.StateDirectory, "state.bin"), []byte("unreadable-private-state"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			build, digest := runtimeMetadata(t)
			r, err := NewWindows(c, build, digest, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.App.Start(budget(t)); err == nil {
				t.Fatal("unprepared state accepted")
			}
			if r.ipc.listener != nil || r.workers.Healthy() {
				t.Fatal("failed startup opened IPC/worker")
			}
			if mode == "missing" {
				if _, err := os.Stat(filepath.Join(c.StateDirectory, "state.bin")); !os.IsNotExist(err) {
					t.Fatal("startup initialized missing state", err)
				}
			}
			// The failed hook must release its file/lock handles itself.
			backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
			if err != nil {
				t.Fatal("failed startup retained state lock", err)
			}
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWindowsRuntimeSCMCallbackUsesActualApplication(t *testing.T) {
	if testing.Short() {
		t.Skip("separate SCM callback fixture; never registers or starts a service")
	}
	for _, mode := range []string{"clean", "listener_failure"} {
		t.Run(mode, func(t *testing.T) {
			c := stateFixtureConfig(t)
			seedState(t, c)
			build, digest := runtimeMetadata(t)
			r, err := NewWindows(c, build, digest, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			host, err := windowsservice.New(r.App, windowsservice.Config{StartTimeout: c.StartTimeout, StopTimeout: c.StopTimeout, StatusTimeout: c.StatusTimeout, CheckpointInterval: c.CheckpointInterval})
			if err != nil {
				t.Fatal(err)
			}
			commands := make(chan svc.ChangeRequest, 1)
			statuses := make(chan svc.Status, 64)
			done := make(chan uint32, 1)
			go func() { _, code := host.Execute(nil, commands, statuses); done <- code }()
			t.Cleanup(func() { close(commands) })
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			waiting := true
			for waiting {
				select {
				case status := <-statuses:
					waiting = status.State != svc.Running
				case code := <-done:
					t.Fatal("SCM callback failed before Running", code)
				case <-timer.C:
					t.Fatal("SCM callback startup exceeded bound")
				}
			}
			connection, err := localipc.Connect(budget(t), r.PipeConfig())
			if err != nil {
				t.Fatal(err)
			}
			_ = connection.Close()
			if mode == "listener_failure" {
				if err := r.ipc.listener.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				commands <- svc.ChangeRequest{Cmd: svc.Stop}
			}
			select {
			case code := <-done:
				if mode == "clean" && code != 0 || mode == "listener_failure" && code == 0 {
					t.Fatal("SCM stop outcome lost mandatory failure", mode, code)
				}
			case <-timer.C:
				t.Fatal("SCM callback stop exceeded bound")
			}
			if r.workers.Healthy() {
				t.Fatal("SCM reported stop with live workers")
			}
			assertPipeClosed(t, r.PipeConfig())
		})
	}

}
