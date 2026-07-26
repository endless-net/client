//go:build windows

package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsConfigStateUsesDPAPIMachineProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := SaveConfig(path, Config{
		ControlPlaneURLs:   []string{"https://api.example.test"},
		IdentityPrivateKey: "identity-secret",
		PrivateKey:         "wireguard-secret",
		NodeCredential:     "credential-secret",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"identity-secret", "wireguard-secret", "credential-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("protected client state contains plaintext %q: %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), windowsConfigProtectionProvider) {
		t.Fatalf("protected client state does not identify DPAPI provider: %s", raw)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PrivateKey != "wireguard-secret" || loaded.NodeCredential != "credential-secret" {
		t.Fatalf("unprotected config = %#v", loaded)
	}
}

func TestWindowsConfigStateRejectsPlainJSON(t *testing.T) {
	if _, err := unprotectConfigState([]byte(`{"state_format":"endlessnet-client-state","state_version":1}`)); err == nil {
		t.Fatal("plain JSON client state was accepted without DPAPI protection")
	}
}
