package client

import (
	"path/filepath"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCBundlePurgePreservesRecoveryAndRemovesDurableOrphans(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	op, err := m.createBundleAs(peer, &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "bundles.state")
	s := NewClientRPCService(m, nil)
	s.bundleStore, err = openClientRPCBundleStore(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.bundleStore.putID(op.Id, peer.Identity, peer.Identity, profile.ProfileId, []byte("recoverable")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.bundleStore.put(peer.Identity, profile.ProfileId, []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	if err := s.purgeBundles(); err != nil {
		t.Fatal(err)
	}
	s.bundleStore, err = openClientRPCBundleStore(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.bundleStore.items) != 1 || s.bundleStore.items[op.Id].metadata == nil {
		t.Fatal("purge lost pending artifact or retained orphan")
	}
	// Read-side expiry prunes memory first; sweep must still persist the deletion.
	now = now.Add(15 * time.Minute)
	_, err = s.bundleStore.read(peer.Identity, profile.ProfileId, &ipc.ReadDiagnosticsBundleRequest{BundleId: op.Id})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	if err := s.purgeBundles(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(-15 * time.Minute)
	restored, err := openClientRPCBundleStore(path, clock)
	if err != nil || len(restored.items) != 0 {
		t.Fatal("expired bytes remained in durable file", err)
	}
}
