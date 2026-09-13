package main

import (
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestSignedSharingMapRequiresFreshGrantAfterExpiry(t *testing.T) {
	key := testMapSigningKey(t)
	m := testNetworkMapWithRevision(t, key, "recipient", "node", 3)
	now := time.Now().UTC()
	peer := api.Peer{ID: "source", Hostname: "source", NetworkID: "source-network", PublicKey: testWireGuardPublicKey("source"), AllowedIPs: []string{"100.65.0.1/32"}}
	m.Peers = []api.Peer{peer}
	m.Network.SharePeerGrants = []api.SharePeerGrant{{
		GrantID: "share", RecipientNetworkID: m.Network.ID,
		RecipientNodeID: m.Node.ID, RecipientPublicKey: m.Node.PublicKey,
		RecipientAllowedIPs: []string{"100.64.0.2/32"},
		SourceNetworkID:     peer.NetworkID, SourceNodeID: peer.ID,
		SourcePublicKey: peer.PublicKey, SourceAllowedIPs: peer.AllowedIPs,
		Rights:     []api.ShareTraffic{{Protocol: "tcp", FirstPort: 443, LastPort: 443}},
		Initiation: api.ShareInitiationRecipientOnly, Revision: 1,
		IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(30 * time.Second),
	}}
	var err error
	m.MapSignature, err = api.SignNetworkMap(key, m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{NodeID: m.Node.ID, NetworkID: m.Network.ID, MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, m.MapSignature))}
	if err := verifyNetworkMapAt(&cfg, m, now); err != nil {
		t.Fatalf("fresh sharing map: %v", err)
	}
	afterExpiry := now.Add(31 * time.Second)
	if !afterExpiry.Before(m.MapSignature.ExpiresAt) {
		t.Fatal("fixture map signature expires before the sharing lease check")
	}
	if err := verifyNetworkMapAt(&cfg, m, afterExpiry); err == nil {
		t.Fatal("expired sharing grant accepted despite its expired lease")
	}
	m.Network.SharePeerGrants[0].Revision++
	m.Network.SharePeerGrants[0].ExpiresAt = now.Add(time.Minute)
	if err := verifyNetworkMapAt(&cfg, m, afterExpiry); err == nil {
		t.Fatal("unsigned sharing lease extension accepted")
	}
	m.MapSignature, err = api.SignNetworkMap(key, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNetworkMapAt(&cfg, m, afterExpiry); err != nil {
		t.Fatalf("fresh signed sharing lease rejected: %v", err)
	}
}
