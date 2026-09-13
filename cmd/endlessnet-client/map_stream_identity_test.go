package main

import (
	"encoding/json"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestStreamRejectsSignedForeignIdentityBeforeCacheMutation(t *testing.T) {
	for _, kind := range []string{"snapshot", "delta"} {
		for _, identity := range []string{"node", "network"} {
			t.Run(kind+"/"+identity, func(t *testing.T) {
				key := testMapSigningKey(t)
				base := testNetworkMapWithRevision(t, key, "net-1", "node-1", 3)
				base.Revision = api.MapRevision{Network: 3, Global: 7}
				event := testMapStreamSnapshotEvent(t, base)
				cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, event.ResultSignature))}
				if _, _, err := cacheNetworkMapFromEvent(&cfg, event); err != nil {
					t.Fatal(err)
				}
				foreign := *event.Snapshot
				foreign.Revision.Network++
				foreign.Network.Revision++
				if identity == "node" {
					foreign.Node.ID = "foreign-node"
				} else {
					foreign.Network.ID = "foreign-net"
					foreign.Node.NetworkID = "foreign-net"
				}
				signature, err := api.SignNetworkMapSnapshot(key, foreign)
				if err != nil {
					t.Fatal(err)
				}
				next := event
				next.Type, next.To, next.ResultSignature = kind, foreign.Revision, signature
				next.Snapshot = &foreign
				if kind == "delta" {
					next.From, next.BaseHash = event.To, event.ResultSignature.PayloadHash
					next.Snapshot = nil
					next.Delta = &api.MapDelta{Network: &foreign.Network, Node: &foreign.Node}
				}
				// Establish that the negative case has a valid producer signature:
				// rejection must come from the client's own identity binding.
				if _, err := api.ApplyMapStreamEvent(cfg.CachedMap.Snapshot(), next, *cfg.MapSigningTrust, time.Now().UTC()); err != nil {
					t.Fatalf("fixture is not a valid signed stream event: %v", err)
				}
				assertStreamRejectedWithoutMutation(t, &cfg, next, time.Now().UTC())
			})
		}
	}
}

func TestStreamReplayRevalidatesCachedProjection(t *testing.T) {
	for _, kind := range []string{"delta", "heartbeat"} {
		for _, scenario := range []string{"valid", "expired", "tampered", "foreign-node"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				base := signedTestNetworkMap(t, "net-1", "node-1", 3)
				base.Revision = api.MapRevision{Network: 3, Global: 7}
				event := testMapStreamSnapshotEvent(t, base)
				cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, event.ResultSignature))}
				if _, _, err := cacheNetworkMapFromEvent(&cfg, event); err != nil {
					t.Fatal(err)
				}
				event.Type, event.Snapshot = kind, nil
				event.From, event.BaseHash = event.To, event.ResultSignature.PayloadHash
				if kind == "delta" {
					event.Delta = &api.MapDelta{Network: &base.Network}
				} else {
					event.ResultSignature = nil
				}
				observedAt := time.Now().UTC()
				switch scenario {
				case "expired":
					observedAt = cfg.CachedMap.MapSignature.ExpiresAt.Add(time.Second)
				case "tampered":
					cfg.CachedMap.Node.Hostname = "unsigned-hostname"
				case "foreign-node":
					cfg.NodeID = "different-enrollment"
				}
				if scenario != "valid" {
					assertStreamRejectedWithoutMutation(t, &cfg, event, observedAt)
					return
				}
				result, _, err := cacheNetworkMapFromEventAt(&cfg, event, observedAt)
				if err != nil || result.Revision.Global != 7 || result.Node.ID != "node-1" {
					t.Fatalf("valid replay lost its verified projection: %v", err)
				}
			})
		}
	}
}

func assertStreamRejectedWithoutMutation(t *testing.T, cfg *client.Config, event api.MapStreamEvent, observedAt time.Time) {
	t.Helper()
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cacheNetworkMapFromEventAt(cfg, event, observedAt); err == nil {
		t.Fatal("unsafe stream projection accepted")
	}
	after, err := json.Marshal(cfg)
	if err != nil || string(before) != string(after) {
		t.Fatal("rejected stream projection mutated client state")
	}
}
