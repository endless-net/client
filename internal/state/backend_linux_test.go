package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxStatePersistenceAndNoReset(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated Linux test environment")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const maxPlain, maxCipher = 65536, 131072
	if _, err := OpenLinux(ctx, directory, false, maxPlain, maxCipher); !errors.Is(err, ErrMissing) {
		t.Fatalf("startup with missing identity: %v", err)
	}
	store, err := OpenLinux(ctx, directory, true, maxPlain, maxCipher)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	installed, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLinux(ctx, directory, false, maxPlain, maxCipher)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Load(ctx)
	if err != nil || current.InstallationID != installed.InstallationID || current.Revision != installed.Revision {
		t.Fatalf("installation identity not preserved: %v", err)
	}
	file := filepath.Join(directory, linuxStateFile)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Initialize(ctx); !errors.Is(err, ErrStale) {
		t.Fatalf("state was reinitialized: %v", err)
	}
	if _, err := reopened.Load(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered state was accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(directory, linuxIdentityFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLinux(ctx, directory, true, maxPlain, maxCipher); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("identity was regenerated beside existing state: %v", err)
	}
}

func TestLinuxStateRejectsUnsafeDirectoryAndHonorsLockDeadline(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root in an isolated Linux test environment")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	backend := LinuxBackend{directory: directory}
	operation := func(Slot) error { return nil }
	if err := backend.Exclusive(t.Context(), operation); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unsafe permissions accepted: %v", err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if err := (LinuxBackend{directory: alias}).Exclusive(t.Context(), operation); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("symlink accepted: %v", err)
	}
	locked, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- backend.Exclusive(t.Context(), func(Slot) error {
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case err := <-finished:
		t.Fatalf("first lock failed: %v", err)
	case <-locked:
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := backend.Exclusive(ctx, operation)
	close(release)
	if first := <-finished; first != nil {
		t.Fatal(first)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock did not respect the deadline: %v", err)
	}
}
