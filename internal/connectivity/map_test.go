package connectivity

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

type announcementsFunc func(context.Context, string) (control.Announcement, error)

type clockFunc func() (ClockSample, error)

func TestRecoveryNeverTreatsUnknownOrExpiredCredentialAsUnlimited(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		f := newMapFixture(t)
		if unknown {
			f.profile.Attempt.CredentialEnds = time.Time{}
		} else {
			f.profile.Attempt.CredentialEnds = f.now
		}
		if _, err := NewVerifier(f.profile, f.config); !errors.Is(err, ErrCredentialExpired) {
			t.Fatal("credential recovery classification lost", unknown, err)
		}
	}
}

func (f clockFunc) Sample() (ClockSample, error) { return f() }

func (f announcementsFunc) Fetch(ctx context.Context, origin string) (control.Announcement, error) {
	return f(ctx, origin)
}

type mapFixture struct {
	profile                 state.Profile
	config                  VerifierConfig
	trust                   control.Announcement
	now                     time.Time
	mapPrivate, nodePrivate ed25519.PrivateKey
	value                   coordinator.NetworkMapSnapshot
}

func newMapFixture(t *testing.T) *mapFixture {
	t.Helper()
	f := &mapFixture{now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	bundle := func(seed byte) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
		private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32))
		public := private.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(public)
		id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
		before, after := f.now.Add(-time.Hour), f.now.Add(2*time.Hour)
		return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public), NotBefore: &before, NotAfter: &after}}}, private
	}
	mapBundle, mapPrivate := bundle(61)
	nodeBundle, nodePrivate := bundle(62)
	relayBundle, _ := bundle(63)
	f.mapPrivate, f.nodePrivate = mapPrivate, nodePrivate
	raw, err := json.Marshal(signing.ServerKeyResponse{TrustBundle: mapBundle, NodeCredentialTrustBundle: nodeBundle, RelayTrustBundle: relayBundle})
	if err != nil {
		t.Fatal(err)
	}
	f.trust, err = control.DecodeAnnouncement("https://control.example", raw, f.now)
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
	a, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: profileID, Owner: "S-1-5-21-1", ControlOrigin: f.trust.Origin, DeviceKeyHash: fingerprint}, f.now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a.Phase = enrollment.Registered
	a.AccountID = "account"
	a.NetworkID = "network"
	a.NodeID = "node"
	a.CredentialEnds = f.now.Add(time.Hour)
	payload, err := coordinator.NodeCredentialSigningPayload(nodeBundle.ActiveKeyID, a.NetworkID, a.NodeID, []string{"node:register", "node:map", "node:endpoint"}, a.CredentialEnds)
	if err != nil {
		t.Fatal(err)
	}
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: nodeBundle.ActiveKeyID, NetworkID: a.NetworkID, NodeID: a.NodeID, Scopes: []string{"node:register", "node:map", "node:endpoint"}, ExpiresAt: a.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(nodePrivate, payload))}
	raw, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	f.profile = state.Profile{ID: profileID, ControlOrigin: f.trust.Origin, Keys: keys, Attempt: &a, Trust: state.ServerTrust{KeyID: f.trust.KeyID, AnnouncementID: f.trust.ID, Bundle: f.trust.Bundle}, Secrets: state.RemoteSecrets{RequestProof: "fixture-request-proof-00000000000000000", RegistrationID: a.RequestID, RegistrationRequest: "original-request", NodeCredential: "enc_" + base64.RawURLEncoding.EncodeToString(raw)}}
	f.profile.Connection = &state.ConnectionState{}
	if _, err := f.profile.Connection.AcceptIntent(state.IntentConnected, f.now); err != nil {
		t.Fatal(err)
	}
	localKey, err := ecdh.X25519().NewPrivateKey(keys.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{64}, 32))
	if err != nil {
		t.Fatal(err)
	}
	expires := f.now.Add(time.Minute)
	f.value = coordinator.NetworkMapSnapshot{Revision: coordinator.MapRevision{Network: 9007199254740993}, Network: coordinator.Network{ID: a.NetworkID, AccountID: a.AccountID, CIDR: "100.70.0.0/24", Revision: 9007199254740993}, Node: coordinator.Node{ID: a.NodeID, NetworkID: a.NetworkID, PublicKey: base64.StdEncoding.EncodeToString(localKey.PublicKey().Bytes()), AssignedIP: "100.70.0.1", ApprovalState: "approved", Status: "online"}, Peers: []coordinator.Peer{{ID: "peer", NetworkID: a.NetworkID, PublicKey: base64.StdEncoding.EncodeToString(peerKey.PublicKey().Bytes()), Endpoint: "192.0.2.1:51820", EndpointGeneration: 9007199254740993, EndpointExpiresAt: &expires, AllowedIPs: []string{"100.70.0.2/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}, IngressACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.1/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 53}}}}}}}
	f.config = VerifierConfig{Now: func() time.Time { return f.now }, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 10, MaxRules: 100, MaxCredentialBytes: 8192, Announcements: announcementsFunc(func(ctx context.Context, origin string) (control.Announcement, error) {
		if _, ok := ctx.Deadline(); !ok || origin != f.profile.ControlOrigin {
			t.Fatal("trust fetch unbounded/foreign")
		}
		return f.trust, nil
	})}
	base := f.now
	f.config.Clock = clockFunc(func() (ClockSample, error) {
		uptime := time.Hour + f.now.Sub(base)
		return ClockSample{Wall: f.now, Uptime: uptime, UpperUptime: uptime, Epoch: "fixture-runtime"}, nil
	})
	return f
}

func (f *mapFixture) sign(t *testing.T, previous *state.MapCursor, issued, expires time.Time) []byte {
	t.Helper()
	key := f.trust.KeyID
	payload, err := coordinator.NetworkMapSigningPayload(f.value, key, issued, expires)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	f.value.MapSignature = &coordinator.MapSignature{Version: 5, Algorithm: "ed25519-network-map-v5", KeyID: key, IssuedAt: issued, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapPrivate, payload))}
	event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + f.value.MapSignature.PayloadHash, To: f.value.Revision, Snapshot: &f.value, ResultSignature: f.value.MapSignature}
	if previous != nil {
		event.Type = "resync"
		event.From = previous.Revision
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerifierAuthenticatesSubjectDirectionExpiryAndProtectsSnapshot(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	m, err := v.Verify(context.Background(), raw, nil)
	if err != nil || m.Denied() || m.Cursor().Revision.Network != 9007199254740993 {
		t.Fatal("valid current map failed", err)
	}
	copyMap, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	copyMap.Peers[0].IngressACLGrants[0].AllowedPorts[0].Port = 22
	unchanged, err := m.Snapshot()
	if err != nil || unchanged.Peers[0].IngressACLGrants[0].AllowedPorts[0].Port != 53 || unchanged.Peers[0].ACLGrants[0].AllowedPorts[0].Port != 443 {
		t.Fatal("consumer mutated verified directed grants", err)
	}
	cursor := m.Cursor()
	if _, err := v.Verify(context.Background(), raw, &cursor); !errors.Is(err, ErrReplay) {
		t.Fatal("replay became confirmation", err)
	}
	f.now = f.now.Add(time.Second)
	end := f.now.Add(time.Minute)
	f.value.Peers[0].EndpointExpiresAt = &end
	next, err := v.Verify(context.Background(), f.sign(t, &cursor, f.now, end), &cursor)
	if err != nil || next.Cursor().Revision != cursor.Revision || !next.Cursor().IssuedAt.After(cursor.IssuedAt) {
		t.Fatal("fresh current same-revision map rejected", err)
	}
}

func TestVerifierRejectsSignedButUnsafeDeviceProfiles(t *testing.T) {
	for _, name := range []string{"foreign-account", "foreign-network", "foreign-node", "foreign-key", "ipv6", "dns", "subnet", "default", "fallback-ports", "unrestricted", "foreign-peer", "self-peer", "duplicate-peer", "duplicate-ip", "ingress-target", "egress-target", "wildcard-port", "protocol", "endpoint-expired", "endpoint-loopback", "endpoint-candidates", "key-expired", "unapproved", "ttl", "credential-bound", "peer-limit", "rule-limit", "denial-with-peers"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			expires := f.now.Add(time.Minute)
			switch name {
			case "foreign-account":
				f.value.Network.AccountID = "foreign"
			case "foreign-network":
				f.value.Network.ID = "foreign"
			case "foreign-node":
				f.value.Node.ID = "foreign"
			case "foreign-key":
				f.value.Node.PublicKey = f.value.Peers[0].PublicKey
			case "ipv6":
				f.value.Node.AssignedIPv6 = "fd00::1"
			case "dns":
				f.value.Network.DNS = []string{"192.0.2.53"}
			case "subnet":
				f.value.Peers[0].AllowedIPs = []string{"100.70.0.0/24"}
			case "default":
				f.value.Peers[0].AllowedIPs = []string{"0.0.0.0/0"}
			case "fallback-ports":
				f.value.Peers[0].AllowedPorts = []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}
			case "unrestricted":
				f.value.Peers[0].ACLRestricted = false
			case "foreign-peer":
				f.value.Peers[0].NetworkID = "other"
			case "self-peer":
				f.value.Peers[0].ID = f.value.Node.ID
			case "duplicate-peer":
				f.value.Peers = append(f.value.Peers, f.value.Peers[0])
			case "duplicate-ip":
				f.value.Peers[0].AllowedIPs = []string{"100.70.0.1/32"}
			case "ingress-target":
				f.value.Peers[0].IngressACLGrants[0].DestinationCIDRs = []string{"100.70.0.2/32"}
			case "egress-target":
				f.value.Peers[0].ACLGrants[0].DestinationCIDRs = []string{"100.70.0.1/32"}
			case "wildcard-port":
				f.value.Peers[0].ACLGrants[0].AllowedPorts = nil
			case "protocol":
				f.value.Peers[0].ACLGrants[0].AllowedPorts[0].Protocol = "icmp"
			case "endpoint-expired":
				end := f.now
				f.value.Peers[0].EndpointExpiresAt = &end
			case "endpoint-loopback":
				f.value.Peers[0].Endpoint = "127.0.0.1:51820"
			case "endpoint-candidates":
				f.value.Peers[0].EndpointCandidates = []string{"192.0.2.2:51820"}
			case "key-expired":
				end := f.now
				f.value.Node.KeyExpiryEnabled = true
				f.value.Node.KeyExpiresAt = &end
			case "unapproved":
				f.value.Node.ApprovalState = "pending"
			case "ttl":
				expires = f.now.Add(301 * time.Second)
			case "credential-bound":
				expires = f.profile.Attempt.CredentialEnds.Add(time.Second)
			case "peer-limit":
				f.config.MaxPeers = 1
				f.value.Peers = append(f.value.Peers, f.value.Peers[0])
			case "rule-limit":
				f.config.MaxRules = 1
			case "denial-with-peers":
				f.value.Node.Status = "revoked"
			}
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			m, err := v.Verify(context.Background(), f.sign(t, nil, f.now, expires), nil)
			if err == nil || !m.ExpiresAt().IsZero() {
				t.Fatal("signed unsafe map accepted")
			}
		})
	}
}

func TestVerifierDenialAndDenyAllAreDifferentFromUnavailableAuthority(t *testing.T) {
	for _, name := range []string{"denial", "deny-all", "outage", "changed-trust", "wrong-purpose", "outer-signature", "outer-revision", "expired", "future"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			v, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.value.Peers = []coordinator.Peer{}
			if name == "denial" {
				f.value.Node.Status = "revoked"
				f.value.Node.ApprovalState = "pending"
			}
			raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
			switch name {
			case "outage":
				v.config.Announcements = announcementsFunc(func(context.Context, string) (control.Announcement, error) {
					return control.Announcement{}, errors.New("secret-trust-diagnostic")
				})
			case "changed-trust":
				f.trust.ID = "sha256:other"
			case "wrong-purpose":
				var trust signing.ServerKeyResponse
				_ = json.Unmarshal(f.trust.Bundle, &trust)
				key := trust.NodeCredentialTrustBundle.ActiveKeyID
				payload, err := coordinator.NetworkMapSigningPayload(f.value, key, f.now, f.now.Add(time.Minute))
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(payload)
				var event coordinator.MapStreamEvent
				_ = json.Unmarshal(raw, &event)
				sig := event.Snapshot.MapSignature
				sig.KeyID = key
				sig.PayloadHash = hex.EncodeToString(hash[:])
				sig.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.nodePrivate, payload))
				event.ResultSignature = sig
				raw, _ = json.Marshal(event)
			case "outer-signature":
				var event coordinator.MapStreamEvent
				_ = json.Unmarshal(raw, &event)
				event.ResultSignature.Signature = "changed"
				raw, _ = json.Marshal(event)
			case "outer-revision":
				var event coordinator.MapStreamEvent
				_ = json.Unmarshal(raw, &event)
				event.To.Network++
				raw, _ = json.Marshal(event)
			case "expired":
				f.now = f.now.Add(time.Minute)
			case "future":
				f.now = f.now.Add(-time.Second)
			}
			m, err := v.Verify(context.Background(), raw, nil)
			if name == "denial" || name == "deny-all" {
				if err != nil || m.Denied() != (name == "denial") {
					t.Fatal("denial/deny-all semantics lost", err)
				}
			} else if err == nil || !m.ExpiresAt().IsZero() || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe authority became new map or leaked diagnostics", err)
			}
		})
	}
}

func TestVerifierRejectsRollbackAndDoesNotPromoteControlEvents(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := v.Verify(context.Background(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor := first.Cursor()
	f.value.Revision.Network--
	f.value.Network.Revision = f.value.Revision.Network
	if _, err := v.Verify(context.Background(), f.sign(t, &cursor, f.now, f.now.Add(time.Minute)), &cursor); !errors.Is(err, ErrStale) {
		t.Fatal("network rollback accepted", err)
	}
	for _, kind := range []string{"heartbeat", "checkpoint"} {
		event := coordinator.MapStreamEvent{Type: kind, ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "event", From: cursor.Revision, To: cursor.Revision}
		raw, _ := json.Marshal(event)
		if _, err := v.Verify(context.Background(), raw, &cursor); !errors.Is(err, ErrNoMap) {
			t.Fatal("control event became authoritative confirmation", err)
		}
	}
}

func TestVerifierCancellationProtocolRecoveryAndSafeDiagnostics(t *testing.T) {
	f := newMapFixture(t)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", v, v), f.profile.Secrets.NodeCredential) {
		t.Fatal("verifier formatting exposes credential")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Verify(ctx, f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled map accepted", err)
	}
	event := coordinator.MapStreamEvent{Type: "delta", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "delta", BaseHash: strings.Repeat("a", 64), Delta: &coordinator.MapDelta{}, ResultSignature: &coordinator.MapSignature{}}
	raw, _ := json.Marshal(event)
	if _, err := v.Verify(context.Background(), raw, nil); !errors.Is(err, ErrResync) {
		t.Fatal("unreconstructed delta became authority", err)
	}
	if _, err := NewVerifier(state.Profile{}, f.config); !errors.Is(err, ErrConfig) {
		t.Fatal("unbound verifier accepted")
	}
}
