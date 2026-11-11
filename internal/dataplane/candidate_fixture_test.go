package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

type candidateAnnouncements struct{ value control.Announcement }

func (a candidateAnnouncements) Fetch(context.Context, string) (control.Announcement, error) {
	return a.value, nil
}

// Unit-only keys and signed maps exercise the real producer canonicalizer and
// Client verifier. This fixture grants no native or external-network authority.
type candidateFixture struct {
	clock    *probeClock
	profile  state.Profile
	snapshot coordinator.NetworkMapSnapshot
	mapKey   ed25519.PrivateKey
	trust    control.Announcement
	lease    *connectivity.Lease
	verified connectivity.Map
	planTTL  time.Duration
}

func newCandidateFixture(t *testing.T) *candidateFixture {
	t.Helper()
	now := time.Now().UTC()
	f := &candidateFixture{clock: &probeClock{sample: connectivity.ClockSample{Wall: now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "candidate-unit"}}}
	bundle := func(seed byte) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32))
		public := key.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(public)
		id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
		before, after := now.Add(-time.Hour), now.Add(2*time.Hour)
		return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public), NotBefore: &before, NotAfter: &after}}}, key
	}
	maps, mapKey := bundle(81)
	nodes, nodeKey := bundle(82)
	relays, _ := bundle(83)
	f.mapKey = mapKey
	raw, err := json.Marshal(signing.ServerKeyResponse{TrustBundle: maps, NodeCredentialTrustBundle: nodes, RelayTrustBundle: relays})
	if err != nil {
		t.Fatal(err)
	}
	f.trust, err = control.DecodeAnnouncement("https://control.example", raw, now)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := state.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	const profileID = "862e4ef0-fe90-4b89-bbbd-6e5f67bf8c56"
	attempt, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: profileID, Owner: "S-1-5-21-1", ControlOrigin: f.trust.Origin, DeviceKeyHash: fingerprint}, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	attempt.Phase = enrollment.Registered
	attempt.AccountID = "account"
	attempt.NetworkID = "network"
	attempt.NodeID = "node"
	attempt.CredentialEnds = now.Add(time.Hour)
	scopes := []string{"node:register", "node:map", "node:endpoint"}
	payload, err := coordinator.NodeCredentialSigningPayload(nodes.ActiveKeyID, "network", "node", scopes, attempt.CredentialEnds)
	if err != nil {
		t.Fatal(err)
	}
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: nodes.ActiveKeyID, NetworkID: "network", NodeID: "node", Scopes: scopes, ExpiresAt: attempt.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(nodeKey, payload))}
	raw, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	f.profile = state.Profile{ID: profileID, ControlOrigin: f.trust.Origin, Keys: keys, Attempt: &attempt, Trust: state.ServerTrust{KeyID: f.trust.KeyID, AnnouncementID: f.trust.ID, Bundle: f.trust.Bundle}, Secrets: state.RemoteSecrets{NodeCredential: "enc_" + base64.RawURLEncoding.EncodeToString(raw)}, Connection: &state.ConnectionState{}}
	if _, err := f.profile.Connection.AcceptIntent(state.IntentConnected, now); err != nil {
		t.Fatal(err)
	}
	localKey, err := ecdh.X25519().NewPrivateKey(keys.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{84}, 32))
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Minute)
	profile := cipherProfile(transport.AmneziaWG)
	profile.Prelude = []transport.Datagram{{Segments: []transport.Segment{{Bytes: []byte{1, 2}}, {Random: 4}}}}
	digest, err := transport.ProfileDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	caps := transport.Capabilities{Version: 1, Engines: []transport.Capability{{Engine: transport.AmneziaWG, ProtocolVersion: "3.1", ProfileVersion: 1}}}
	localAd := &coordinator.TransportAdvertisement{IntentGeneration: 1, Capabilities: caps, Endpoints: []coordinator.TransportEndpoint{{ProfileID: profile.ID, ProfileDigest: digest, Endpoint: "192.0.2.10:51821"}}}
	peerAd := coordinator.TransportAdvertisement{IntentGeneration: 4, Capabilities: caps, Endpoints: []coordinator.TransportEndpoint{{ProfileID: profile.ID, ProfileDigest: digest, Endpoint: "192.0.2.20:51820"}}}
	local := coordinator.Node{ID: "node", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(localKey.PublicKey().Bytes()), AssignedIP: "100.70.0.1", ApprovalState: "approved", Status: "online", EndpointExpiresAt: &expires, Transport: localAd}
	peer := coordinator.Peer{ID: "peer", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(peerKey.PublicKey().Bytes()), Endpoint: "192.0.2.20:51820", EndpointGeneration: 1, EndpointExpiresAt: &expires, AllowedIPs: []string{"100.70.0.2/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}}
	revision := coordinator.MapRevision{Network: 1}
	peer.Transport = &coordinator.PeerTransportPlan{Version: coordinator.PeerTransportPlanVersion, Availability: coordinator.TransportAvailable, AccountID: "account", NetworkID: "network", LocalNodeID: local.ID, LocalPublicKey: local.PublicKey, PeerNodeID: peer.ID, PeerPublicKey: peer.PublicKey, LocalIntentGeneration: 1, Revision: revision, IssuedAt: now, ExpiresAt: expires, PeerAdvertisement: peerAd, Profiles: []coordinator.TransportProfileOffer{{Profile: profile, Digest: digest, LocalEndpoint: localAd.Endpoints[0].Endpoint, PeerEndpoint: peerAd.Endpoints[0].Endpoint}}}
	f.snapshot = coordinator.NetworkMapSnapshot{Revision: revision, Network: coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.70.0.0/24", Revision: 1}, Node: local, Peers: []coordinator.Peer{peer}}
	f.lease, err = connectivity.NewLease(t.Context(), f.clock, profileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.refresh(t, nil)
	return f
}

func (f *candidateFixture) refresh(t *testing.T, previous *state.MapCursor) {
	t.Helper()
	f.verify(t, previous)
	if err := f.lease.Confirm(f.verified); err != nil {
		t.Fatal(err)
	}
}

// Verify before quiescing native policy, as the actual map worker does. The
// old signed lease remains current while untrusted input is being checked.
func (f *candidateFixture) verify(t *testing.T, previous *state.MapCursor) {
	t.Helper()
	sample, _ := f.clock.Sample()
	now := sample.Wall
	expires := now.Add(45 * time.Second)
	for i := range f.snapshot.Peers {
		f.snapshot.Peers[i].Transport.IssuedAt = now
		f.snapshot.Peers[i].Transport.ExpiresAt = expires
		if f.planTTL > 0 {
			f.snapshot.Peers[i].Transport.ExpiresAt = minTime(expires, now.Add(f.planTTL))
		}
		f.snapshot.Peers[i].Transport.Revision = f.snapshot.Revision
	}
	payload, err := coordinator.NetworkMapSigningPayload(f.snapshot, f.trust.KeyID, now, expires)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	signature := &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: f.trust.KeyID, IssuedAt: now, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapKey, payload))}
	f.snapshot.MapSignature = signature
	event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + signature.PayloadHash, To: f.snapshot.Revision, Snapshot: &f.snapshot, ResultSignature: signature}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := connectivity.NewVerifier(f.profile, connectivity.VerifierConfig{Now: func() time.Time { s, _ := f.clock.Sample(); return s.Wall }, Clock: f.clock, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192, Announcements: candidateAnnouncements{f.trust}})
	if err != nil {
		t.Fatal(err)
	}
	f.verified, err = verifier.VerifySnapshot(t.Context(), raw, previous)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *candidateFixture) assignment(t *testing.T) candidateAssignment {
	t.Helper()
	sample, _ := f.clock.Sample()
	a, err := newCandidateAssignment(f.verified, f.lease, "peer", "fixture-profile", sample.Wall)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
