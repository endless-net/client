//go:build windows

package wintundriver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryLockProtectsVerifiedFileAndAncestorIdentity(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "application")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "wintun.dll")
	content := []byte("read-only loader locking fixture, never executed")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, unlock, err := lockBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if data, err := io.ReadAll(file); err != nil || string(data) != string(content) {
		t.Fatal("wrong file locked", err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err == nil {
		t.Fatal("verified bytes could change while loading")
	}
	if err := os.Remove(path); err == nil {
		t.Fatal("verified file could be replaced")
	}
	if err := os.Rename(directory, filepath.Join(root, "moved")); err == nil {
		t.Fatal("ancestor identity could change during load")
	}
	unlock()
	if err := os.WriteFile(path, []byte("replacement after unlock"), 0o600); err != nil {
		t.Fatal("read-only guard changed persistent ACL or leaked a handle", err)
	}
}

func TestBinaryLockRejectsNoncanonicalPathsAndDirectories(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "wintun.dll")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, "wintun.dll", `\\server\share\wintun.dll`, filepath.Join(root, "missing", "wintun.dll"), filepath.Join(root, "other.dll")} {
		_, unlock, err := lockBinary(path)
		if unlock != nil {
			unlock()
		}
		if !errors.Is(err, ErrDriver) {
			t.Fatal("unsafe loader path accepted", path, err)
		}
	}
}
