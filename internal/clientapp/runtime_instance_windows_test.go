//go:build windows

package clientapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestRuntimeInstancePublicationAndOwnedRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "runtime-instance")
	first := "34b5fbe6-9641-4c73-8ae4-974123fa2e52"
	second := "258d48c3-f86f-4b7f-9d14-53da3b76a628"
	for _, instance := range []string{first, second} {
		if publishRuntimeInstance(ctx, path, instance, time.Millisecond) != nil {
			t.Fatal("publish")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != instance+"\n" {
			t.Fatal("partial or stale instance", err)
		}
	}
	if removeRuntimeInstance(path, first) != nil {
		t.Fatal("old instance cleanup")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != second+"\n" {
		t.Fatal("old instance removed new address")
	}
	for range 2 {
		if removeRuntimeInstance(path, second) != nil {
			t.Fatal("current instance cleanup")
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("instance retained after successful stop", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatal("publication left temporary files", err)
	}
}

func TestRuntimeInstancePublicationRefusesInvalidOrCancelledInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "runtime-instance")
	instance := "34b5fbe6-9641-4c73-8ae4-974123fa2e52"
	for _, invalid := range []string{"", "not-an-instance", "00000000-0000-0000-0000-000000000000"} {
		if publishRuntimeInstance(ctx, path, invalid, time.Millisecond) == nil {
			t.Fatal("invalid instance published")
		}
	}
	if publishRuntimeInstance(context.Background(), path, instance, time.Millisecond) == nil || publishRuntimeInstance(ctx, path, instance, 0) == nil {
		t.Fatal("unbounded publication accepted")
	}
	cancel()
	if publishRuntimeInstance(ctx, path, instance, time.Millisecond) == nil {
		t.Fatal("cancelled publication accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed publication created address")
	}
}

func TestWindowsConfigRuntimeInstanceFileIsExplicitAndLocal(t *testing.T) {
	environment := configEnvironment()
	environment["ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE"] = filepath.Join(t.TempDir(), "runtime-instance")
	if _, err := ParseWindows(environment); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"relative", `\\server\share\runtime-instance`, environment["ENDLESSNET_CLIENT_STATE_DIRECTORY"]} {
		environment["ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE"] = invalid
		if _, err := ParseWindows(environment); err == nil {
			t.Fatal("invalid publication path accepted")
		}
	}
}

func TestRuntimeInstanceReaderCannotCausePartialPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-instance")
	first := "34b5fbe6-9641-4c73-8ae4-974123fa2e52"
	second := "258d48c3-f86f-4b7f-9d14-53da3b76a628"
	if os.WriteFile(path, []byte(first+"\n"), 0600) != nil {
		t.Fatal("fixture")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reader := os.NewFile(uintptr(handle), path)
	t.Cleanup(func() { _ = reader.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if publishRuntimeInstance(ctx, path, second, time.Millisecond) != ErrIPC || ctx.Err() == nil {
		t.Fatal("locked address was replaced or transient lock did not respect budget")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != first+"\n" {
		t.Fatal("failed publication damaged previous address")
	}
	if reader.Close() != nil {
		t.Fatal("release fixture reader")
	}
	fresh, freshCancel := context.WithTimeout(context.Background(), time.Second)
	defer freshCancel()
	if publishRuntimeInstance(fresh, path, second, time.Millisecond) != nil {
		t.Fatal("released address could not be published")
	}
}
