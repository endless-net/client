package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

// Coordinator CI supplies the actual base and Topic-delivered grant/withdrawal
// deltas. This uses the production CLI/agent cache consumer, not a copied parser.
func TestSharingBackendMapConsumer(t *testing.T) {
	if testing.Short() {
		t.Skip("backend integration runs in CI")
	}
	path := os.Getenv("CLIENT_SHARING_BACKEND_FIXTURE")
	if path == "" {
		path = "testdata/sharing-backend-fixture.json"
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var fixture struct {
		Base       clientapi.NetworkMapSnapshot
		Trust      clientapi.SigningTrustBundle
		Events     []clientapi.MapStreamEvent
		ObservedAt []time.Time
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Events) != 2 || len(fixture.ObservedAt) != 2 || fixture.Base.MapSignature == nil || len(fixture.Base.Network.SharePeerGrants) != 0 {
		t.Fatal("expected backend base, grant and withdrawal")
	}
	base := networkMapResponseFromSnapshot(fixture.Base)
	cfg := client.Config{NodeID: base.Node.ID, NetworkID: base.Network.ID, MapRevision: fixture.Base.Revision.Network, MapGlobalRevision: fixture.Base.Revision.Global, MapHash: fixture.Base.MapSignature.PayloadHash, MapSigningTrust: &fixture.Trust, CachedMap: &base}
	for index, event := range fixture.Events {
		before, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if event.Type != "delta" || event.ResultSignature == nil {
			t.Fatal("expected signed backend delta")
		}
		// A tampered backend envelope must not partially mutate the cache.
		tampered := event
		signature := *event.ResultSignature
		signature.PayloadHash = "invalid"
		tampered.ResultSignature = &signature
		if _, _, err := cacheNetworkMapFromEventAt(&cfg, tampered, fixture.ObservedAt[index]); err == nil {
			t.Fatal("tampered map accepted")
		}
		after, err := json.Marshal(cfg)
		if err != nil || string(before) != string(after) {
			t.Fatal("rejected map changed cache", err)
		}
		result, action, err := cacheNetworkMapFromEventAt(&cfg, event, fixture.ObservedAt[index])
		if err != nil {
			t.Fatal(err)
		}
		want := 1 - index
		if action != "delta" || len(result.Network.SharePeerGrants) != want || len(result.Peers) != want || cfg.MapHash != event.ResultSignature.PayloadHash || cfg.MapRevision != event.To.Network {
			t.Fatal("backend sharing state not applied to cache")
		}
		replayed, replayAction, err := cacheNetworkMapFromEventAt(&cfg, event, fixture.ObservedAt[index])
		if err != nil || replayAction != "unchanged" {
			t.Fatal("cached sharing map failed replay", err)
		}
		if replayed.Node.ID != result.Node.ID || replayed.Network.ID != result.Network.ID || len(replayed.Peers) != want || len(replayed.Network.SharePeerGrants) != want {
			t.Fatal("repeated delta lost the effective map")
		}
	}
}
