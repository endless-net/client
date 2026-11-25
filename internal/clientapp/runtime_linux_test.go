package clientapp

import (
	"context"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
)

func TestLinuxRuntimeBootstrapsAuthenticatedCLI(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated Linux test environment")
	}
	stateDir, runDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{stateDir, runDir} {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := state.OpenLinux(t.Context(), stateDir, true, linuxMessageBytes, 2*linuxMessageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{1}
	build := &clientv0.BuildIdentity{Version: "fixture", Commit: "fixture", Platform: clientv0.Platform_PLATFORM_LINUX, Architecture: runtime.GOARCH}
	app, err := newLinuxApp(build, digest, io.Discard, stateDir, runDir)
	if err != nil {
		t.Fatal(err)
	}
	start, cancel := context.WithTimeout(t.Context(), linuxStartTimeout)
	err = app.Start(start)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), linuxStopTimeout)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
		if _, err := localipc.ReadLinuxInstance(runDir); err == nil {
			t.Error("runtime hint survived service stop")
		}
	})
	instance, err := localipc.ReadLinuxInstance(runDir)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := localipc.ConnectLinux(t.Context(), localipc.LinuxSocketConfig{
		Directory: runDir, InstanceID: instance, ContractDigest: digest, HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	response, err := clientv0.NewClientServiceClient(conn).GetRuntimeInfo(t.Context(), &clientv0.GetRuntimeInfoRequest{})
	if err != nil || response.GetRuntime().GetBuild().GetPlatform() != clientv0.Platform_PLATFORM_LINUX {
		t.Fatalf("Linux runtime not published by the authenticated controller: %v", err)
	}
}
