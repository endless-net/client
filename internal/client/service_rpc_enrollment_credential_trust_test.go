package client

import (
	"crypto/ed25519"
	"encoding/base64"
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestEnrollmentCheckpointPersistsCredentialVerificationAuthority(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := api.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	credential, err := api.SignNodeCredential(private, "network", "node", []string{"node:map"}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	m, op, initial := enrollmentCheckpointTest(t)
	save := m.EnrollmentSaveCallback(op.Id, initial)
	initial.NodeID, initial.NetworkID = "node", "network"
	initial.NodeCredential, initial.NodeCredentialSigningTrust = credential, &trust
	if err := save(initial); err != nil {
		t.Fatal(err)
	}
	// A later checkpoint must compare against the trust saved by the previous
	// one, rather than either dropping it or treating its own update as stale.
	initial.NodeApprovalState = "approved"
	if err := save(initial); err != nil {
		t.Fatal(err)
	}
	configStores.Delete(m.store.path)
	reopened, err := OpenConfigStore(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	stored := reopened.Read()
	if stored.NodeCredential != credential || !reflect.DeepEqual(stored.NodeCredentialSigningTrust, &trust) {
		t.Fatal("credential and its verification authority were not persisted together")
	}
	if _, err := api.VerifyNodeCredentialWithTrustBundle(stored.NodeCredential, *stored.NodeCredentialSigningTrust, "node:map", now); err != nil {
		t.Fatal("persisted credential cannot be verified after reopening", err)
	}
}

func TestEnrollmentCheckpointRejectsConcurrentCredentialTrustChange(t *testing.T) {
	m, op, initial := enrollmentCheckpointTest(t)
	save := m.EnrollmentSaveCallback(op.Id, initial)
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := api.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeCredentialSigningTrust = &trust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial.NodeID = "late-node"
	assertRPCFailure(t, save(initial), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	stored := m.store.Read()
	if stored.NodeID != "" || !reflect.DeepEqual(stored.NodeCredentialSigningTrust, &trust) || m.Metadata().Revision != op.Metadata.Revision {
		t.Fatal("stale enrollment checkpoint overwrote credential trust or advanced operation")
	}
}
