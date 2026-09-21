package main

import (
	"encoding/base64"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestMapCacheReconcilesRetiredHostChoicesAtomically(t *testing.T) {
	key := testMapSigningKey(t)
	previous := testNetworkMapWithRevision(t, key, "net-1", "node-1", 3)
	previous.Peers = []api.Peer{{ID: "resource-host", Hostname: "host", PublicKey: testWireGuardPublicKey("resource-host"), AllowedIPs: []string{"100.64.0.3/32"}}}
	var err error
	previous.MapSignature, err = api.SignNetworkMap(key, previous)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, previous.MapSignature))}
	if err := cacheNetworkMapChecked(&cfg, previous); err != nil {
		t.Fatal(err)
	}
	id := base64.RawURLEncoding.EncodeToString([]byte("1\x00resource-host"))
	cfg.ResourcePreferences = map[string]bool{id: false}
	next := previous
	next.Peers = nil
	next.Network.Revision++
	next.Revision.Network = next.Network.Revision
	next.MapSignature, err = api.SignNetworkMap(key, next)
	if err != nil {
		t.Fatal(err)
	}
	tampered := next
	tampered.Network.Name = "tampered"
	before := cfg
	if err := cacheNetworkMapChecked(&cfg, tampered); err == nil {
		t.Fatal("unverified refresh changed resource choices")
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("rejected cache mutation was not atomic")
	}
	if err := cacheNetworkMapChecked(&cfg, next); err != nil {
		t.Fatal(err)
	}
	if cfg.ResourcePreferences != nil || len(cfg.ResourcePreferencesRetired) != 1 || cfg.ResourcePreferencesRetired[id] || cfg.MapHash != next.MapSignature.PayloadHash {
		t.Fatal("map and retired choice were not installed together")
	}
}

func TestApprovedMapRestoresChoicesAfterSameNodePendingClearsCache(t *testing.T) {
	response := signedTestNetworkMap(t, "net-1", "node-1", 3)
	response.Peers = []api.Peer{{ID: "resource-host", Hostname: "host", PublicKey: testWireGuardPublicKey("resource-host"), AllowedIPs: []string{"100.64.0.2/32"}}}
	var err error
	response.MapSignature, err = api.SignNetworkMap(testMapSigningKey(t), response)
	if err != nil {
		t.Fatal(err)
	}
	id := base64.RawURLEncoding.EncodeToString([]byte("1\x00resource-host"))
	missing := base64.RawURLEncoding.EncodeToString([]byte("1\x00retired-host"))
	// Match same-node pending enrollment: retain identities and local choices,
	// clear CachedMap and MapRevision, retain the global cursor/hash.
	cfg := client.Config{NodeID: response.Node.ID, NetworkID: response.Network.ID, NodeApprovalState: "pending", MapHash: response.MapSignature.PayloadHash,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature)), ResourcePreferences: map[string]bool{id: false, missing: false}}
	invalid := response
	invalid.Network.Name = "tampered"
	if err := cacheNetworkMapChecked(&cfg, invalid); err == nil {
		t.Fatal("missing old cache bypassed signature verification")
	}
	if cfg.CachedMap != nil || len(cfg.ResourcePreferences) != 2 {
		t.Fatal("invalid approved map changed choices")
	}
	if err := cacheNetworkMapChecked(&cfg, response); err != nil {
		t.Fatal("same-node approval blocked by missing old cache", err)
	}
	if cfg.CachedMap == nil || len(cfg.ResourcePreferences) != 1 || cfg.ResourcePreferences[id] || len(cfg.ResourcePreferencesRetired) != 1 || cfg.ResourcePreferencesRetired[missing] {
		t.Fatal("approved map lost active or retired saved intent")
	}
}
