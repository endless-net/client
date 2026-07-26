package client

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func writeLegacyConfigStateForTest(t *testing.T, path string, raw []byte) []byte {
	t.Helper()
	protected, err := protectLegacyConfigStateForTest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, protected, 0o600); err != nil {
		t.Fatal(err)
	}
	return protected
}

func TestMigrateConfigStateV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	backupPath := path + ".backup"
	source := writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 2,
		"server_url": "https://primary.example.test/",
		"server_urls": ["https://fallback.example.test", "https://primary.example.test"],
		"token": "session-token",
		"active_account_id": "account-1",
		"identity_private_key": "identity-private-key",
		"private_key": "wireguard-private-key",
		"node_id": "node-1",
		"network_id": "network-1",
		"node_credential": "node-credential",
		"node_approval_state": "approved",
		"enrollment_request_id": "request-1",
		"enrollment_poll_token": "poll-token",
		"enrollment_approval_url": "https://approve.example.test/request-1",
		"device_fingerprint": "device-fingerprint",
		"map_revision": 42,
		"subnet_router_snat": true,
		"exit_lan_policy": "block",
		"wireguard_mtu": 1380,
		"wireguard_route_table": "100"
	}`))

	result, err := MigrateConfigState(path, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated ||
		result.SourceFormat != "legacy" ||
		result.SourceVersion != LegacyConfigStateVersion ||
		result.TargetFormat != CurrentConfigStateFormat ||
		result.TargetVersion != CurrentConfigStateVersion ||
		result.BackupPath != backupPath {
		t.Fatalf("migration result = %#v", result)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, source) {
		t.Fatal("migration backup does not match the original protected state")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(backupPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("migration backup mode = %04o, want 0600", info.Mode().Perm())
		}
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.StateFormat != CurrentConfigStateFormat ||
		got.StateVersion != CurrentConfigStateVersion ||
		!reflect.DeepEqual(got.ControlPlaneURLs, []string{"https://primary.example.test", "https://fallback.example.test"}) ||
		got.Token != "session-token" ||
		got.ActiveAccountID != "account-1" ||
		got.IdentityPrivateKey != "identity-private-key" ||
		got.PrivateKey != "wireguard-private-key" ||
		got.NodeID != "node-1" ||
		got.NetworkID != "network-1" ||
		got.NodeCredential != "node-credential" ||
		got.NodeApprovalState != "approved" ||
		got.EnrollmentRequestID != "request-1" ||
		got.EnrollmentPollToken != "poll-token" ||
		got.ApprovalURL != "https://approve.example.test/request-1" ||
		got.DeviceFingerprint != "device-fingerprint" ||
		got.MapRevision != 42 ||
		!got.SubnetRouterSNAT ||
		got.ExitLANPolicy != "block" ||
		got.WireGuardMTU != 1380 ||
		got.WireGuardRouteTable != "100" {
		t.Fatalf("migrated config = %#v", got)
	}

	second, err := MigrateConfigState(path, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if second.Migrated || second.SourceFormat != CurrentConfigStateFormat || second.SourceVersion != CurrentConfigStateVersion {
		t.Fatalf("second migration result = %#v, want current no-op", second)
	}
}

func TestMigrateConfigStateReusesIdenticalBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	source := writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 2,
		"server_url": "https://api.example.test",
		"token": "",
		"private_key": ""
	}`))
	backupPath := path + ".backup"
	if err := os.WriteFile(backupPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := MigrateConfigState(path, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated {
		t.Fatalf("migration result = %#v, want migrated", result)
	}
}

func TestMigrateConfigStateRejectsUnknownLegacyFieldsWithoutChangingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	source := writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 2,
		"server_url": "https://api.example.test",
		"token": "",
		"private_key": "",
		"unknown": true
	}`))
	backupPath := path + ".backup"
	if _, err := MigrateConfigState(path, backupPath); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("MigrateConfigState unknown field error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, source) {
		t.Fatal("rejected migration changed the source state")
	}
	if _, err := os.Stat(backupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected migration created backup: %v", err)
	}
}

func TestMigrateConfigStateRejectsUnsupportedLegacyVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 1,
		"server_url": "https://api.example.test",
		"token": "",
		"private_key": ""
	}`))
	_, err := MigrateConfigState(path, "")
	if err == nil || !strings.Contains(err.Error(), "version 2 is required for migration") {
		t.Fatalf("MigrateConfigState unsupported version error = %v", err)
	}
}

func TestMigrateConfigStateRequiresStoppedAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	source := writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 2,
		"server_url": "https://api.example.test",
		"token": "",
		"private_key": ""
	}`))
	lockPath, err := AgentLockPath(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireAgentLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	if _, err := MigrateConfigState(path, ""); err == nil || !errors.Is(err, ErrAgentAlreadyRunning) {
		t.Fatalf("MigrateConfigState active agent error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, source) {
		t.Fatal("active-agent rejection changed the source state")
	}
}

func TestMigrateConfigStateRejectsDifferentExistingBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	source := writeLegacyConfigStateForTest(t, path, []byte(`{
		"state_version": 2,
		"server_url": "https://api.example.test",
		"token": "",
		"private_key": ""
	}`))
	backupPath := path + ".backup"
	if err := os.WriteFile(backupPath, []byte("different state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateConfigState(path, backupPath); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("MigrateConfigState existing backup error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, source) {
		t.Fatal("backup conflict changed the source state")
	}
}

func TestMigrateConfigStateRejectsNonCurrentNamedFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := writeConfigStateForTest(path, []byte(`{
		"state_format": "other-client-state",
		"state_version": 1,
		"token": "",
		"private_key": ""
	}`)); err != nil {
		t.Fatal(err)
	}
	_, err := MigrateConfigState(path, "")
	if err == nil || !IsClientStateFormatUnsupported(err) {
		t.Fatalf("MigrateConfigState named format error = %v", err)
	}
}
