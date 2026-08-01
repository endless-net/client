package client

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestEnrollmentRecoverySurvivesProcessRestartWithStableIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	store, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	recovery, err := NewEnrollmentRecovery("op-1", "reg-1", "https://control.example.test", "key-2", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *Config) error {
		cfg.IdentityPrivateKey = "identity-private-key"
		cfg.PrivateKey = "wireguard-private-key"
		cfg.NodeCredential = "old-node-credential"
		cfg.EnrollmentRecovery = &recovery
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveConfigPath(path)
	if err != nil {
		t.Fatal(err)
	}
	configStores.Delete(resolved)
	restarted, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := restarted.Read()
	if got.EnrollmentRecovery == nil || got.EnrollmentRecovery.OperationID != "op-1" || got.EnrollmentRecovery.IdempotencyID != "reg-1" {
		t.Fatalf("restarted recovery = %#v", got.EnrollmentRecovery)
	}
	if got.NodeCredential != "old-node-credential" || got.IdentityPrivateKey != "identity-private-key" || got.PrivateKey != "wireguard-private-key" {
		t.Fatal("persisting recovery changed enrollment or device keys")
	}
}

func TestRecoveryAndLogoutRetentionMatrix(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	intent := &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, UpdatedAt: now.Format(time.RFC3339)}
	trust := &clientapi.SigningTrustBundle{ActiveKeyID: "trusted-key"}
	recovery, err := NewEnrollmentRecovery("op-1", "reg-1", "https://control.example.test", "key-2", now)
	if err != nil {
		t.Fatal(err)
	}
	base := Config{
		LocalOwnerID:        "uid:1000",
		ControlPlaneURLs:    []string{"https://control.example.test"},
		ManagementURL:       "https://management.example.test",
		Token:               "session-token",
		ActiveAccountID:     "account-1",
		IdentityPrivateKey:  "identity-private-key",
		PrivateKey:          "wireguard-private-key",
		DeviceFingerprint:   "installation-fingerprint",
		MapSigningTrust:     trust,
		ConnectionIntent:    intent,
		NodeID:              "node-1",
		NetworkID:           "network-1",
		NodeCredential:      "node-credential",
		NodeApprovalState:   "approved",
		EnrollmentRequestID: "enrollment-1",
		EnrollmentPollToken: "poll-token",
		ApprovalURL:         "https://approval.example.test",
		EnrollmentRequest:   &clientapi.RegisterNodeRequest{Hostname: "host"},
		MapRevision:         7,
		MapGlobalRevision:   9,
		MapHash:             "map-hash",
		CachedMap:           &clientapi.RegisterNodeResponse{},
		CachedMapSavedAt:    &now,
		EnrollmentRecovery:  &recovery,
	}

	terminal := clonePersistentConfig(base)
	if err := ApplyTerminalRecoveryCleanup(&terminal); err != nil {
		t.Fatal(err)
	}
	assertPreservedRecoveryMaterial(t, terminal, base)
	if terminal.Token != base.Token || terminal.ActiveAccountID != base.ActiveAccountID || !reflect.DeepEqual(terminal.ConnectionIntent, base.ConnectionIntent) {
		t.Fatal("terminal recovery did not preserve session and connection intent")
	}
	assertNodeBoundStateCleared(t, terminal)

	logout := clonePersistentConfig(base)
	if err := ApplyLocalLogoutCleanup(&logout, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertPreservedRecoveryMaterial(t, logout, base)
	if logout.Token != "" || logout.ActiveAccountID != "" {
		t.Fatal("local logout retained user session")
	}
	if logout.ConnectionIntent == nil || logout.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || logout.ConnectionIntent.Reason != "local_logout" {
		t.Fatalf("local logout intent = %#v", logout.ConnectionIntent)
	}
	assertNodeBoundStateCleared(t, logout)
}

func assertPreservedRecoveryMaterial(t *testing.T, got, want Config) {
	t.Helper()
	if got.LocalOwnerID != want.LocalOwnerID || !reflect.DeepEqual(got.ControlPlaneURLs, want.ControlPlaneURLs) ||
		got.ManagementURL != want.ManagementURL || got.IdentityPrivateKey != want.IdentityPrivateKey ||
		got.PrivateKey != want.PrivateKey || got.DeviceFingerprint != want.DeviceFingerprint ||
		!reflect.DeepEqual(got.MapSigningTrust, want.MapSigningTrust) {
		t.Fatal("cleanup changed preserved owner/key/trust/control material")
	}
}

func assertNodeBoundStateCleared(t *testing.T, got Config) {
	t.Helper()
	if got.NodeID != "" || got.NetworkID != "" || got.NodeCredential != "" || got.NodeApprovalState != "" ||
		got.EnrollmentRequestID != "" || got.EnrollmentPollToken != "" || got.ApprovalURL != "" ||
		got.EnrollmentRequest != nil || got.MapRevision != 0 || got.MapGlobalRevision != 0 || got.MapHash != "" ||
		got.CachedMap != nil || got.CachedMapSavedAt != nil || got.EnrollmentRecovery != nil {
		t.Fatalf("node-bound state was retained: %#v", got)
	}
}

func TestEnrollmentRecoveryFailureKeepsOperationIdentity(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	recovery, err := NewEnrollmentRecovery("op-1", "reg-1", "https://control.example.test", "key-2", now)
	if err != nil {
		t.Fatal(err)
	}
	failed := recovery.WithFailure(RecoveryPhasePolicyBlocked, "authorization_denied", "req-403", false, now.Add(time.Second))
	if err := failed.Validate(); err != nil {
		t.Fatal(err)
	}
	if failed.OperationID != recovery.OperationID || failed.IdempotencyID != recovery.IdempotencyID || failed.ConfirmedKeyID != recovery.ConfirmedKeyID {
		t.Fatalf("failure replaced stable recovery identity: %#v", failed)
	}
	if failed.ErrorCode != "authorization_denied" || failed.RequestID != "req-403" || failed.Retryable {
		t.Fatalf("failure classification = %#v", failed)
	}
}

func TestEnrollmentRecoveryRejectsIncompleteOrUnknownState(t *testing.T) {
	if _, err := NewEnrollmentRecovery("", "reg-1", "https://control.example.test", "key-2", time.Now()); err == nil {
		t.Fatal("incomplete recovery was accepted")
	}
	value, err := NewEnrollmentRecovery("op-1", "reg-1", "https://control.example.test", "key-2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value.Phase = RecoveryPhase("future")
	if err := value.Validate(); err == nil {
		t.Fatal("unknown recovery phase was accepted")
	}
}
