//go:build linux || darwin

package client

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigAliasesShareStoreAndAgentLock(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "client.json")
	if err := SaveConfig(original, Config{}); err != nil {
		t.Fatal(err)
	}
	fileAlias := filepath.Join(dir, "alias.json")
	dirAlias := filepath.Join(t.TempDir(), "directory-alias")
	if err := os.Symlink(original, fileAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, dirAlias); err != nil {
		t.Fatal(err)
	}
	store, err := OpenConfigStore(original)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := AgentLockPath(original)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireAgentLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	for _, alias := range []string{fileAlias, filepath.Join(dirAlias, "client.json")} {
		aliasStore, err := OpenConfigStore(alias)
		if err != nil || aliasStore != store {
			t.Fatal("alias created a separate configuration store")
		}
		aliasLock, err := AgentLockPath(alias)
		if err != nil || aliasLock != lockPath {
			t.Fatal("alias created a separate lock location")
		}
		duplicate, err := AcquireAgentLock(aliasLock)
		if err == nil {
			_ = duplicate.Close()
			t.Fatal("alias bypassed active agent ownership")
		}
		if !errors.Is(err, ErrAgentAlreadyRunning) {
			t.Fatal("alias failed for a reason other than ownership")
		}
	}
	if err := store.Update(func(cfg *Config) error { cfg.ManagementURL = "https://management.example.test"; return nil }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(fileAlias)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("configuration write replaced the alias")
	}
	fresh := filepath.Join(dir, "missing", "nested", "client.json")
	freshAlias := filepath.Join(dirAlias, "missing", "nested", "client.json")
	a, err := AgentLockPath(fresh)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AgentLockPath(freshAlias)
	if err != nil || a != b {
		t.Fatal("missing profile under an aliased directory has a different lock")
	}
	broken := filepath.Join(dir, "broken.json")
	if err := os.Symlink(filepath.Join(dir, "absent.json"), broken); err != nil {
		t.Fatal(err)
	}
	if _, err := AgentLockPath(broken); err == nil {
		t.Fatal("dangling configuration symlink was accepted")
	}
}
