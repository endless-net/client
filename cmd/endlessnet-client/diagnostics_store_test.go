package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsStoreReusesAndBoundsOwnedFiles(t *testing.T) {
	dir := t.TempDir()
	prepareDiagnosticsTestDirectory(t, dir)
	store := newDiagnosticsStore(dir)
	first, err := store.Write(map[string]any{"status": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	reused, err := store.Write(map[string]any{"status": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Path != first.Path || !reused.CreatedAt.Equal(first.CreatedAt) || !reused.ExpiresAt.Equal(first.ExpiresAt) || reused.SizeBytes != first.SizeBytes {
		t.Fatalf("reused bundle = %#v, first = %#v", reused, first)
	}
	unrelated := filepath.Join(dir, "keep-me.txt")
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	last := first
	for i := 0; i < diagnosticsBundleMaxFiles+2; i++ {
		old := time.Now().Add(-2 * diagnosticsBundleReuseWindow)
		if err := os.Chtimes(last.Path, old, old); err != nil {
			t.Fatal(err)
		}
		last, err = store.Write(map[string]any{"iteration": i})
		if err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(dir, "diagnostics-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != diagnosticsBundleMaxFiles {
		t.Fatalf("owned bundle count = %d, want %d", len(matches), diagnosticsBundleMaxFiles)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

func TestDiagnosticsStoreRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := newDiagnosticsStore(filepath.Join(alias, "diagnostics")).Prune()
	if err == nil || !strings.Contains(err.Error(), "link or reparse point") {
		t.Fatalf("Prune with symlinked parent error = %v", err)
	}
}

func TestDiagnosticsStorePrunesExpiredAndRejectsOwnedSymlink(t *testing.T) {
	dir := t.TempDir()
	prepareDiagnosticsTestDirectory(t, dir)
	store := newDiagnosticsStore(dir)
	bundle, err := store.Write(map[string]any{"status": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-diagnosticsBundleRetention - time.Hour)
	if err := os.Chtimes(bundle.Path, expired, expired); err != nil {
		t.Fatal(err)
	}
	if err := store.Prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundle.Path); !os.IsNotExist(err) {
		t.Fatalf("expired bundle still exists: %v", err)
	}
	name, err := newDiagnosticsBundleName(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := store.Prune(); err == nil {
		t.Fatal("Prune accepted an owned-name symlink")
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "outside" {
		t.Fatalf("symlink target changed: raw=%q err=%v", raw, err)
	}
}
