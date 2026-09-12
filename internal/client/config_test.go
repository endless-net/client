package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeConfigStateForTest(path string, raw []byte) error {
	if runtime.GOOS == "windows" {
		protected, err := protectConfigState(raw)
		if err != nil {
			return err
		}
		raw = protected
	}
	return os.WriteFile(path, raw, 0o600)
}

func TestDefaultConfigPathUsesCanonicalLinuxState(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only default path")
	}
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != DefaultLinuxServiceConfigPath {
		t.Fatalf("DefaultConfigPath() = %q, want %q", path, DefaultLinuxServiceConfigPath)
	}
}

func TestOpenConfigStoreRejectsRemovedLinuxStatePath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only removed path")
	}
	if _, err := OpenConfigStore("/etc/endlessnet/client.json"); err == nil || !strings.Contains(err.Error(), DefaultLinuxServiceConfigPath) {
		t.Fatalf("OpenConfigStore removed Linux path error = %v", err)
	}
}

func TestConfigStoreMergesConcurrentTopLevelUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := SaveConfig(path, Config{ControlPlaneURLs: []string{"https://api.example.test"}, Token: "token"}); err != nil {
		t.Fatal(err)
	}

	agentConfig, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	ipcConfig, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	agentConfig.MapRevision = 42
	if err := SaveConfig(path, agentConfig); err != nil {
		t.Fatal(err)
	}
	ipcConfig.NodeApprovalState = "approved"
	if err := SaveConfig(path, ipcConfig); err != nil {
		t.Fatal(err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.MapRevision != 42 || got.NodeApprovalState != "approved" {
		t.Fatalf("merged config = %#v, want map revision and approval update", got)
	}
}

func TestConfigStoreMergesUpdateWrittenByAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := SaveConfig(path, Config{ControlPlaneURLs: []string{"https://api.example.test"}, Token: "old-token"}); err != nil {
		t.Fatal(err)
	}

	stale, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	external := clonePersistentConfig(stale)
	external.MapRevision = 42
	if err := saveConfigFile(path, external); err != nil {
		t.Fatal(err)
	}
	stale.Token = "new-token"
	if err := SaveConfig(path, stale); err != nil {
		t.Fatal(err)
	}

	got, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "new-token" || got.MapRevision != 42 {
		t.Fatalf("merged config token=%q revision=%d, want new-token and 42", got.Token, got.MapRevision)
	}
}

func TestConfigStoreUpdateReloadsChangesWrittenByAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	store, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.Token = "old-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	external := clonePersistentConfig(store.Read())
	external.Token = "new-token"
	if err := saveConfigFile(path, external); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.MapRevision = 42
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "new-token" || got.MapRevision != 42 {
		t.Fatalf("updated config token=%q revision=%d, want new-token and 42", got.Token, got.MapRevision)
	}
}

func TestConfigStoreUpdatePersistsOneSynchronizedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	store, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.NodeID = "node-1"
		cfg.NodeCredential = "credential-1"
		cfg.MapRevision = 7
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := store.Read()
	if got.NodeID != "node-1" || got.NodeCredential != "credential-1" || got.MapRevision != 7 {
		t.Fatalf("stored config = %#v", got)
	}
}

func TestLoadConfigRequiresStateVersionOne(t *testing.T) {
	for _, version := range []int{0, 2, 999} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			raw := []byte(fmt.Sprintf(`{"state_format":"%s","state_version":%d,"control_plane_urls":["https://api.example.test"]}`, CurrentConfigStateFormat, version))
			if err := writeConfigStateForTest(path, raw); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "version 1 is required") || !IsClientStateVersionUnsupported(err) {
				t.Fatalf("LoadConfig state version %d error = %v", version, err)
			}
		})
	}
}

func TestLoadConfigRequiresCurrentStateFormat(t *testing.T) {
	for name, raw := range map[string]string{
		"missing": `{"state_version":1}`,
		"old":     `{"state_format":"endlessnet-client-state-v2","state_version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			if err := writeConfigStateForTest(path, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil || !IsClientStateFormatUnsupported(err) {
				t.Fatalf("LoadConfig state format error = %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsRemovedAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"state_format":"endlessnet-client-state","state_version":1,"server_url":"removed"}`,
		`{"state_format":"endlessnet-client-state","state_version":1,"server_urls":["removed"]}`,
		`{"state_format":"endlessnet-client-state","state_version":1,"enrollment_approval_url":"removed"}`,
		`{"state_format":"endlessnet-client-state","state_version":1,"map_signing_public_key":"removed"}`,
		`{"state_format":"endlessnet-client-state","state_version":1,"unknown":true}`,
		`{"state_format":"endlessnet-client-state","state_version":1} {"state_format":"endlessnet-client-state","state_version":1}`,
	} {
		path := filepath.Join(t.TempDir(), "client.json")
		if err := writeConfigStateForTest(path, []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatalf("LoadConfig accepted non-canonical state %s", raw)
		}
	}
}

func TestSaveConfigAtomicallyReplacesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "client.json")
	if err := SaveConfig(path, Config{ControlPlaneURLs: []string{"https://old.example.test"}, Token: "old-token"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{
		LocalOwnerID:       "uid:1000",
		ControlPlaneURLs:   []string{"https://new.example.test"},
		Token:              "new-token",
		IdentityPrivateKey: "identity-private-key",
		NodeID:             "node-1",
		NodeCredential:     "credential-1",
		MapRevision:        3,
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("saved config is not valid JSON: %s", raw)
	}
	if runtime.GOOS != "windows" {
		state := string(raw)
		if !strings.Contains(state, `"state_format": "endlessnet-client-state"`) ||
			!strings.Contains(state, `"state_version": 1`) || strings.Contains(state, `"server_url"`) {
			t.Fatalf("saved config does not use canonical state v1: %s", state)
		}
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalOwnerID != "uid:1000" || !reflect.DeepEqual(loaded.ControlPlaneURLs, []string{"https://new.example.test"}) || loaded.Token != "new-token" || loaded.IdentityPrivateKey != "identity-private-key" || loaded.NodeID != "node-1" || loaded.MapRevision != 3 {
		t.Fatalf("loaded config = %#v", loaded)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("config mode = %v, want 0600", got)
		}
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary config files left behind: %v", matches)
	}
}

func TestConfigControlPlaneURLsKeepsPrimaryAndDeduplicates(t *testing.T) {
	cfg := Config{
		ControlPlaneURLs: []string{"https://primary.example.test/", " https://fallback-a.example.test/ ", "https://primary.example.test", "", "https://fallback-a.example.test"},
	}
	got := cfg.ControlURLs()
	want := []string{"https://primary.example.test", "https://fallback-a.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("control plane URLs = %#v, want %#v", got, want)
	}
}

func TestExitLANPolicyNormalization(t *testing.T) {
	for input, want := range map[string]string{
		"":        "",
		" allow ": ExitLANPolicyAllow,
		"BLOCK":   ExitLANPolicyBlock,
	} {
		got, err := NormalizeExitLANPolicy(input)
		if err != nil {
			t.Fatalf("NormalizeExitLANPolicy(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeExitLANPolicy(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := NormalizeExitLANPolicy("bypass"); err == nil || !strings.Contains(err.Error(), "exit_lan_policy") {
		t.Fatalf("NormalizeExitLANPolicy invalid error = %v", err)
	}
	block, err := ExitLANPolicyBlocksLocalLAN(ExitLANPolicyBlock)
	if err != nil || !block {
		t.Fatalf("ExitLANPolicyBlocksLocalLAN(block) = %v, %v; want true, nil", block, err)
	}
}

func TestWireGuardMTUNormalization(t *testing.T) {
	for input, want := range map[int]int{
		0:     0,
		1280:  1280,
		65535: 65535,
	} {
		got, err := NormalizeWireGuardMTU(input)
		if err != nil {
			t.Fatalf("NormalizeWireGuardMTU(%d): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeWireGuardMTU(%d) = %d, want %d", input, got, want)
		}
	}
	for _, input := range []int{-1, 1279, 65536} {
		if _, err := NormalizeWireGuardMTU(input); err == nil || !strings.Contains(err.Error(), "wireguard_mtu") {
			t.Fatalf("NormalizeWireGuardMTU(%d) error = %v, want wireguard_mtu validation", input, err)
		}
	}
}

func TestWireGuardRouteTableNormalization(t *testing.T) {
	for input, want := range map[string]string{
		"":       "",
		" off ":  "off",
		"AUTO":   "auto",
		"000100": "100",
	} {
		got, err := NormalizeWireGuardRouteTable(input)
		if err != nil {
			t.Fatalf("NormalizeWireGuardRouteTable(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeWireGuardRouteTable(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"0", "-1", "+100", "main", "abc"} {
		if _, err := NormalizeWireGuardRouteTable(input); err == nil || !strings.Contains(err.Error(), "wireguard_route_table") {
			t.Fatalf("NormalizeWireGuardRouteTable(%q) error = %v, want wireguard_route_table validation", input, err)
		}
	}
}

func TestLoadConfigRejectsGroupReadableSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are not represented by POSIX mode bits")
	}
	path := filepath.Join(t.TempDir(), "client.json")
	raw := []byte(`{"state_format":"endlessnet-client-state","state_version":1,"control_plane_urls":["https://api.example.test"],"identity_private_key":"secret-identity-private-key","private_key":"secret-private-key","node_credential":"node-secret"}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "must not be readable or writable by group or other users") {
		t.Fatalf("LoadConfig error = %v, want unsafe permission rejection", err)
	}
}

func TestLoadConfigAllowsGroupReadablePublicConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are not represented by POSIX mode bits")
	}
	path := filepath.Join(t.TempDir(), "client.json")
	raw := []byte(`{"state_format":"endlessnet-client-state","state_version":1,"control_plane_urls":["https://api.example.test"]}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.ControlPlaneURLs, []string{"https://api.example.test"}) {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func TestSaveConfigRemovesStaleAtomicTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	staleTemp := filepath.Join(dir, ".client.json.tmp-stale")
	freshTemp := filepath.Join(dir, ".client.json.tmp-fresh")
	if err := os.WriteFile(staleTemp, []byte("interrupted old write"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(staleTemp, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshTemp, []byte("possible concurrent write"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveConfig(path, Config{ControlPlaneURLs: []string{"https://recover.example.test"}, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staleTemp); !os.IsNotExist(err) {
		t.Fatalf("stale atomic temp file still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(freshTemp); err != nil {
		t.Fatalf("fresh atomic temp file was removed: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.ControlPlaneURLs, []string{"https://recover.example.test"}) || loaded.Token != "token" {
		t.Fatalf("loaded recovered config = %#v", loaded)
	}
}

func TestLoadConfigIgnoresInterruptedAtomicTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	if err := SaveConfig(path, Config{ControlPlaneURLs: []string{"https://stable.example.test"}, Token: "stable-token"}); err != nil {
		t.Fatal(err)
	}
	interruptedTemp := filepath.Join(dir, ".client.json.tmp-interrupted")
	if err := os.WriteFile(interruptedTemp, []byte(`{"server_url":`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.ControlPlaneURLs, []string{"https://stable.example.test"}) || loaded.Token != "stable-token" {
		t.Fatalf("loaded config = %#v, want stable target despite interrupted temp file", loaded)
	}
}

func TestWriteFileAtomicFailurePreservesExistingTargetAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	err := WriteFileAtomic(path, []byte(`{"server_url":"https://new.example.test"}`), 0o600)
	if err == nil {
		t.Fatal("WriteFileAtomic unexpectedly replaced a directory target")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if !info.IsDir() {
		t.Fatalf("failed atomic write replaced target with mode %v", info.Mode())
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, ".client.json.tmp-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("failed atomic write left temporary files: %v", matches)
	}
}
