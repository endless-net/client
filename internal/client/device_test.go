package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	wgkeys "github.com/unng-lab/endlessnet/clientapi/wireguard"
)

func TestDeviceFingerprintUsesHostLocalInstallationID(t *testing.T) {
	deviceADir := filepath.Join(t.TempDir(), "device-a")
	setUserConfigDirForTest(t, deviceADir)

	first, err := DeviceFingerprint("https://api.example.test/", "identity-public-key")
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeviceFingerprint("https://api.example.test", "identity-public-key")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("fingerprint changed for the same installation: first=%s second=%s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("fingerprint length = %d, want 64 hex chars", len(first))
	}

	path, err := installationStatePath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state installationState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(state.InstallationID) == "" {
		t.Fatalf("installation id was not persisted in %s", path)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("installation state mode = %v, want 0600", got)
		}
	}

	setUserConfigDirForTest(t, filepath.Join(t.TempDir(), "device-b"))
	third, err := DeviceFingerprint("https://api.example.test", "identity-public-key")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatalf("fingerprint should differ for a different installation id: %s", third)
	}
}

func TestBindConfigDeviceFingerprintRejectsCopiedState(t *testing.T) {
	var cfg Config
	changed, err := BindConfigDeviceFingerprint(&cfg, "fingerprint-a")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || cfg.DeviceFingerprint != "fingerprint-a" {
		t.Fatalf("bound config changed=%v cfg=%#v", changed, cfg)
	}
	changed, err = BindConfigDeviceFingerprint(&cfg, "fingerprint-a")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("rebinding same fingerprint reported changed")
	}
	_, err = BindConfigDeviceFingerprint(&cfg, "fingerprint-b")
	if err == nil || !strings.Contains(err.Error(), "different installation fingerprint") {
		t.Fatalf("mismatch error = %v, want copied-state rejection", err)
	}
}

func TestDeviceFingerprintForConfigBindsComputedFingerprint(t *testing.T) {
	setUserConfigDirForTest(t, filepath.Join(t.TempDir(), "device-bind"))
	privateKey, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{ControlPlaneURLs: []string{"https://api.example.test"}, PrivateKey: privateKey}
	fingerprint, err := DeviceFingerprintForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := BindConfigDeviceFingerprint(&cfg, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || strings.TrimSpace(cfg.DeviceFingerprint) == "" {
		t.Fatalf("bound current device changed=%v cfg=%#v", changed, cfg)
	}
	if err := ValidateConfigCurrentDevice(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.DeviceFingerprint = "copied-from-another-installation"
	if err := ValidateConfigCurrentDevice(cfg); err == nil || !strings.Contains(err.Error(), "different installation fingerprint") {
		t.Fatalf("mismatched config validation err = %v, want copied-state rejection", err)
	}
}

func setUserConfigDirForTest(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("PROGRAMDATA", dir)
		t.Setenv("APPDATA", dir)
		return
	}
	if runtime.GOOS == "linux" {
		t.Setenv("ENDLESSNET_INSTALLATION_STATE_DIR", dir)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
}
