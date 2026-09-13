package main

import (
	"os"
	"path/filepath"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestHTTPMapCachePreservesGlobalStreamCursor(t *testing.T) {
	response := signedTestNetworkMap(t, "net-1", "node-1", 3)
	response.Revision = api.MapRevision{Network: 3, Global: 12}
	var err error
	response.MapSignature, err = api.SignNetworkMap(testMapSigningKey(t), response)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature))}
	if err := cacheNetworkMapChecked(&cfg, response); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := client.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	// Read actual persisted bytes, not the process-wide ConfigStore cache.
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedPath := filepath.Join(t.TempDir(), "reopened.json")
	if err := client.WriteFileAtomic(reopenedPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := client.LoadConfig(reopenedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedCachedNetworkMap(&restored); err != nil {
		t.Fatal(err)
	}
	cursor := mapStreamCursor(restored, restored.MapRevision)
	if cursor.Revision.Global != 12 || cursor.Revision.Network != 3 || cursor.MapHash != response.MapSignature.PayloadHash {
		t.Fatal("persisted HTTP map lost its stream revision or hash")
	}
	for _, global := range []uint64{0, 11, 13} {
		mismatch := restored
		mismatch.MapGlobalRevision = global
		if _, err := verifiedCachedNetworkMap(&mismatch); err == nil {
			t.Fatal("cache accepted a mismatched global revision")
		}
	}
	for _, global := range []uint64{0, 11} {
		stale := response
		stale.Revision.Global = global
		stale.MapSignature, err = api.SignNetworkMap(testMapSigningKey(t), stale)
		if err != nil {
			t.Fatal(err)
		}
		if err := cacheNetworkMapChecked(&restored, stale); err == nil {
			t.Fatal("HTTP refresh rolled back the global policy revision")
		}
		if restored.MapGlobalRevision != 12 || restored.MapHash != response.MapSignature.PayloadHash {
			t.Fatal("rejected refresh mutated the persisted cursor")
		}
	}
	updated := response
	updated.Revision.Global = 13
	updated.MapSignature, err = api.SignNetworkMap(testMapSigningKey(t), updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := cacheNetworkMapChecked(&restored, updated); err != nil {
		t.Fatal(err)
	}
	cursor = mapStreamCursor(restored, restored.MapRevision)
	if cursor.Revision.Global != 13 || cursor.MapHash != updated.MapSignature.PayloadHash {
		t.Fatal("global-only refresh did not replace the stream cursor")
	}
}
