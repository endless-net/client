//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
)

func initializationCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "client.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "-ldflags",
		"-X main.version=000000000000 -X main.commit=0000000000000000000000000000000000000000 -X main.buildDate=2026-11-01T00:00:00Z", "./cmd/client")
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build isolated CLI: %v: %s", err, output)
	}
	return binary
}

func invokeInitializationCLI(t *testing.T, binary string, config Config, arguments ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "ENDLESSNET_CLIENT_") {
			command.Env = append(command.Env, entry)
		}
	}
	environment := configEnvironment()
	environment["ENDLESSNET_CLIENT_STATE_DIRECTORY"] = config.StateDirectory
	environment["ENDLESSNET_CLIENT_SERVICE_SID"] = config.ServiceSID
	environment["ENDLESSNET_CLIENT_ALLOWED_SIDS"] = config.ServiceSID
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	return command.CombinedOutput()
}

func loadInitializedState(t *testing.T, c Config) state.State {
	t.Helper()
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	}()
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(backend, protector, c.InstallationID, c.MaxStatePlainBytes, c.MaxStateCipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func checkInitializedAppLifecycle(t *testing.T, c Config) {
	t.Helper()
	build, digest := runtimeMetadata(t)
	app, err := NewWindows(c, build, digest, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.App.Start(budget(t)); err != nil {
		t.Fatal("initialized state cannot start isolated App", err)
	}
	t.Cleanup(func() {
		if err := app.App.Stop(budget(t)); err != nil {
			t.Error(err)
		}
	})
	if err := app.App.Stop(budget(t)); err != nil {
		t.Fatal("initialized App cannot stop", err)
	}
	snapshot := loadInitializedState(t, c)
	if snapshot.Revision != 1 || snapshot.OwnerSID != "" || len(snapshot.Profiles) != 0 {
		t.Fatal("startup silently created owner/profile/attempt")
	}
}

func TestWindowsExplicitInitializationCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("separate native CLI/DPAPI/protected file check")
	}
	binary, c := initializationCLI(t), stateFixtureConfig(t)
	path := filepath.Join(c.StateDirectory, "state.bin")
	for _, arguments := range [][]string{{"init-state", "--force"}, {"reset"}, {"--init-state"}} {
		if output, err := invokeInitializationCLI(t, binary, c, arguments...); err == nil || bytes.Contains(output, []byte(c.StateDirectory)) {
			t.Fatalf("unknown/force command accepted: %v: %s", err, output)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid command created state", err)
		}
	}
	if output, err := invokeInitializationCLI(t, binary, c, "init-state"); err != nil || !bytes.Contains(output, []byte("client empty state initialized")) {
		t.Fatalf("explicit initialization failed: %v: %s", err, output)
	}
	snapshot := loadInitializedState(t, c)
	if snapshot.Version != state.CurrentVersion || snapshot.Revision != 1 || snapshot.InstallationID != c.InstallationID || snapshot.OwnerSID != "" || snapshot.ActiveProfileID != "" || len(snapshot.Profiles) != 0 {
		t.Fatal("initialization created an owner, profile, keys or attempt")
	}
	checkInitializedAppLifecycle(t, c)
	cipher, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range [][]byte{cipher, []byte("foreign-or-corrupt-synthetic-state")} {
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := invokeInitializationCLI(t, binary, c, "init-state")
		if err == nil || !bytes.Contains(output, []byte("client state initialization rejected")) || bytes.Contains(output, []byte(c.StateDirectory)) || bytes.Contains(output, original) {
			t.Fatal("existing state accepted or disclosed", err)
		}
		retained, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(retained, original) {
			t.Fatal("initialization overwrote existing state", err)
		}
	}
}

func TestWindowsInstallerConfigurationValidationDoesNotWriteState(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit CLI process fixture")
	}
	binary := initializationCLI(t)
	c := stateFixtureConfig(t)
	path := filepath.Join(c.StateDirectory, "state.bin")
	if output, err := invokeInitializationCLI(t, binary, c, "validate-config"); err != nil || !bytes.Contains(output, []byte("client configuration valid")) {
		t.Fatalf("configuration validation failed: %v: %s", err, output)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation created state", err)
	}
}

func TestWindowsInitializationRejectsBeforeWriting(t *testing.T) {
	if testing.Short() {
		t.Skip("separate protected directory/OS caller/cancellation check")
	}
	c := stateFixtureConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := InitializeWindowsState(ctx, c); !errors.Is(err, ErrProtectedState) {
		t.Fatal("cancelled initialization accepted", err)
	}
	invalid := c
	invalid.InstallationID = "invalid"
	if err := InitializeWindowsState(context.Background(), invalid); !errors.Is(err, ErrConfig) {
		t.Fatal("invalid configuration accepted", err)
	}
	wrongSID := c
	wrongSID.ServiceSID = "S-1-5-18"
	if c.ServiceSID == wrongSID.ServiceSID {
		wrongSID.ServiceSID = "S-1-5-19"
	}
	if err := InitializeWindowsState(context.Background(), wrongSID); !errors.Is(err, ErrProtectedState) {
		t.Fatal("another service principal accepted", err)
	}
	if entries, err := os.ReadDir(c.StateDirectory); err != nil || len(entries) != 0 {
		t.Fatal("rejected initialization created files", err)
	}
}
