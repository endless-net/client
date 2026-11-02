// Package connectivity verifies current device connectivity before any native
// effects. A verified map is separate from desired intent, OS application and
// path health; no value in this package claims an application resource works.
package connectivity

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"
	"unicode"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

var (
	ErrConfig            = errors.New("invalid connectivity configuration")
	ErrMap               = errors.New("invalid current network map")
	ErrTrust             = errors.New("current connectivity trust unavailable")
	ErrReplay            = errors.New("network map replay")
	ErrStale             = errors.New("network map rollback")
	ErrNoMap             = errors.New("event is not a new authoritative map")
	ErrResync            = errors.New("network map resynchronization required")
	ErrCredentialExpired = errors.New("node credential expired or unknown")
)

type VerifierConfig struct {
	Announcements                                    control.Announcements
	Now                                              func() time.Time
	MaxBytes, MaxPeers, MaxRules, MaxCredentialBytes int
	CallTimeout                                      time.Duration
	Clock                                            Clock
}

type Verifier struct {
	config                                                        VerifierConfig
	profileID, origin, account, network, node, public, credential string
	anchorID                                                      string
	generation                                                    uint64
	floor                                                         *state.MapCursor
	credentialEnds                                                time.Time
}

func (*Verifier) String() string   { return "[private connectivity verifier]" }
func (*Verifier) GoString() string { return "[private connectivity verifier]" }

func NewVerifier(p state.Profile, c VerifierConfig) (*Verifier, error) {
	if c.Clock == nil {
		return nil, ErrConfig
	}
	return newBinding(p, c, true)
}

func newBinding(p state.Profile, c VerifierConfig, connected bool) (*Verifier, error) {
	if p.Connection == nil || p.Connection.Generation == 0 || p.Connection.Intent != state.IntentConnected && p.Connection.Intent != state.IntentDisconnected || connected && p.Connection.Intent != state.IntentConnected || p.Attempt != nil && !pathID(p.Attempt.NodeID) {
		return nil, ErrConfig
	}
	if p.Attempt != nil && c.Now != nil && (p.Attempt.CredentialEnds.IsZero() || !p.Attempt.CredentialEnds.After(c.Now())) {
		return nil, ErrCredentialExpired
	}
	if c.Announcements == nil || c.Now == nil || c.MaxBytes < 1 || c.MaxPeers < 1 || c.MaxRules < 1 || c.MaxCredentialBytes < 1 || c.CallTimeout < time.Millisecond || c.CallTimeout > 30*time.Second || p.Attempt == nil || p.Attempt.Validate() != nil || p.Attempt.Phase != enrollment.Registered || p.Attempt.Uncertain || p.Attempt.ProfileID != p.ID || p.Attempt.ControlOrigin != p.ControlOrigin || p.Trust.AnnouncementID == "" || p.Secrets.NodeCredential == "" {
		return nil, ErrConfig
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil || fingerprint != p.Attempt.DeviceKeyHash {
		return nil, ErrConfig
	}
	private, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil {
		return nil, ErrConfig
	}
	anchor := control.Announcement{Origin: p.ControlOrigin, KeyID: p.Trust.KeyID, ID: p.Trust.AnnouncementID, Bundle: p.Trust.Bundle}
	if anchor.Validate(c.Now()) != nil {
		return nil, ErrTrust
	}
	floor, err := copyCursor(p.Connection.MapCursor)
	if err != nil {
		return nil, err
	}
	return &Verifier{config: c, profileID: p.ID, origin: p.ControlOrigin, account: p.Attempt.AccountID, network: p.Attempt.NetworkID, node: p.Attempt.NodeID, public: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), credential: p.Secrets.NodeCredential, credentialEnds: p.Attempt.CredentialEnds, anchorID: p.Trust.AnnouncementID, generation: p.Connection.Generation, floor: floor}, nil
}

func (v *Verifier) previous(cursor *state.MapCursor) (*state.MapCursor, error) {
	if cursor == nil {
		return copyCursor(v.floor)
	}
	copyValue, err := copyCursor(cursor)
	if err != nil {
		return nil, err
	}
	if v.floor != nil && (copyValue.Revision.Network < v.floor.Revision.Network || copyValue.IssuedAt.Before(v.floor.IssuedAt)) {
		return nil, ErrStale
	}
	if v.floor != nil && copyValue.Revision == v.floor.Revision && copyValue.IssuedAt.Equal(v.floor.IssuedAt) && copyValue.PayloadHash != v.floor.PayloadHash {
		return nil, ErrStale
	}
	return copyValue, nil
}

func (v *Verifier) currentTrust(parent context.Context, scope string) (signing.ServerKeyResponse, error) {
	var empty signing.ServerKeyResponse
	if !v.credentialEnds.After(v.config.Now()) {
		return empty, ErrCredentialExpired
	}
	if parent.Err() != nil {
		return empty, parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, v.config.CallTimeout)
	defer cancel()
	announcement, err := v.config.Announcements.Fetch(ctx, v.origin)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	now := v.config.Now()
	// An HTTPS response or a new self-consistent bundle is not an authorized
	// rotation. The existing explicit trust action must approve its full ID.
	if err != nil || announcement.Origin != v.origin || announcement.ID != v.anchorID || announcement.Validate(now) != nil {
		return empty, ErrTrust
	}
	var trust signing.ServerKeyResponse
	if json.Unmarshal(announcement.Bundle, &trust) != nil {
		return empty, ErrTrust
	}
	claims, err := coordinator.DecodeNodeCredential(v.credential, v.config.MaxCredentialBytes)
	if err != nil {
		return empty, ErrMap
	}
	key, err := signing.ResolveBootstrapKey(trust.NodeCredentialTrustBundle, claims.KeyID, now)
	if err != nil || key.NotAfter != nil && claims.ExpiresAt.After(*key.NotAfter) {
		return empty, ErrTrust
	}
	public, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
	if err != nil {
		return empty, ErrTrust
	}
	verified, err := coordinator.VerifyNodeCredential(v.credential, v.config.MaxCredentialBytes, ed25519.PublicKey(public), v.network, v.node, scope, now)
	if err != nil || !verified.ExpiresAt.Equal(v.credentialEnds) {
		return empty, ErrMap
	}
	return trust, nil
}

// Map is immutable verified input. Its cursor may be durably recorded before
// application, but it is never restored as authority after a daemon restart.
type Map struct {
	snapshot   coordinator.NetworkMapSnapshot
	profileID  string
	anchorID   string
	generation uint64
	denied     bool
	verified   ClockSample
}

func (Map) String() string   { return "[verified connectivity map]" }
func (Map) GoString() string { return "[verified connectivity map]" }

func (m Map) Denied() bool { return m.denied }

// CheckBinding fences a verified result against the latest protected local
// profile. It neither verifies new input nor restores authority from a cursor.
func (m Map) CheckBinding(p state.Profile, generation uint64) error {
	if m.snapshot.MapSignature == nil || p.ID != m.profileID || p.Connection == nil || generation != m.generation || p.Connection.Generation != generation || p.Connection.Intent != state.IntentConnected || p.Trust.AnnouncementID != m.anchorID {
		return state.ErrStale
	}
	return nil
}

// FreshSince accepts only a verifier receipt from after a new session's own
// clock fence. A saved cursor/signature cannot stand in for a post-restart fetch.
func (m Map) FreshSince(sample ClockSample) bool {
	return m.snapshot.MapSignature != nil && sample.valid() && m.verified.valid() && m.verified.Epoch == sample.Epoch && m.verified.Uptime > sample.UpperUptime && !m.verified.Wall.Before(sample.Wall)
}
func (m Map) ExpiresAt() time.Time {
	if m.snapshot.MapSignature == nil {
		return time.Time{}
	}
	return m.snapshot.MapSignature.ExpiresAt
}
func (m Map) Cursor() state.MapCursor {
	if m.snapshot.MapSignature == nil {
		return state.MapCursor{}
	}
	return state.MapCursor{Revision: m.snapshot.Revision, PayloadHash: m.snapshot.MapSignature.PayloadHash, IssuedAt: m.snapshot.MapSignature.IssuedAt}
}

// Snapshot returns a deep copy so an adapter cannot change the verified map or
// another consumer's ACL slices by mutating its local projection.
func (m Map) Snapshot() (coordinator.NetworkMapSnapshot, error) {
	var result coordinator.NetworkMapSnapshot
	if m.snapshot.MapSignature == nil {
		return result, ErrMap
	}
	raw, err := json.Marshal(m.snapshot)
	if err != nil || json.Unmarshal(raw, &result) != nil {
		return coordinator.NetworkMapSnapshot{}, ErrMap
	}
	return result, nil
}

func (v *Verifier) Verify(parent context.Context, raw []byte, previous *state.MapCursor) (Map, error) {
	return v.verify(parent, raw, previous, false)
}

// VerifySnapshot permits a requested full snapshot with From=0, while retaining
// the durable anti-rollback floor. It never treats a saved signature as a grant.
func (v *Verifier) VerifySnapshot(parent context.Context, raw []byte, previous *state.MapCursor) (Map, error) {
	return v.verify(parent, raw, previous, true)
}

func (v *Verifier) verify(parent context.Context, raw []byte, previous *state.MapCursor, full bool) (Map, error) {
	var empty Map
	if parent.Err() != nil {
		return empty, parent.Err()
	}
	previous, err := v.previous(previous)
	if err != nil {
		return empty, err
	}
	event, err := coordinator.DecodeMapStreamEvent(raw, v.config.MaxBytes)
	if err != nil {
		return empty, ErrMap
	}
	if event.Type == "heartbeat" || event.Type == "checkpoint" {
		return empty, ErrNoMap
	}
	if event.Type == "delta" {
		return empty, ErrResync
	}
	value := event.Snapshot
	if value == nil || value.MapSignature == nil || event.ResultSignature == nil || !sameSignature(value.MapSignature, event.ResultSignature) || event.To != value.Revision || value.Revision.Network == 0 || value.Revision.Global != 0 || value.Network.Revision != value.Revision.Network || value.RegistrationBinding != "" {
		return empty, ErrMap
	}
	trust, err := v.currentTrust(parent, "node:map")
	if err != nil {
		return empty, err
	}
	if v.config.Clock == nil {
		return empty, ErrConfig
	}
	sample, clockErr := v.config.Clock.Sample()
	if clockErr != nil || !sample.valid() {
		return empty, ErrClock
	}
	now := sample.Wall
	sig := value.MapSignature
	key, err := signing.ResolveBootstrapKey(trust.TrustBundle, sig.KeyID, now)
	if err != nil || key.NotBefore != nil && sig.IssuedAt.Before(*key.NotBefore) || key.NotAfter != nil && sig.ExpiresAt.After(*key.NotAfter) {
		return empty, ErrTrust
	}
	public, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
	if err != nil || coordinator.VerifyNetworkMap(*value, ed25519.PublicKey(public), v.account, v.network, v.node, v.public, now) != nil || sig.ExpiresAt.Sub(sig.IssuedAt) > 300*time.Second || sig.ExpiresAt.After(v.credentialEnds) {
		return empty, ErrMap
	}
	if previous != nil {
		hash, hashErr := hex.DecodeString(previous.PayloadHash)
		if previous.Revision.Global != 0 || previous.Revision.Network == 0 || previous.IssuedAt.IsZero() || hashErr != nil || len(hash) != 32 || hex.EncodeToString(hash) != previous.PayloadHash {
			return empty, ErrConfig
		}
		if value.Revision.Network < previous.Revision.Network || sig.IssuedAt.Before(previous.IssuedAt) {
			return empty, ErrStale
		}
		if sig.PayloadHash == previous.PayloadHash {
			return empty, ErrReplay
		}
		if value.Revision == previous.Revision && !sig.IssuedAt.After(previous.IssuedAt) {
			return empty, ErrStale
		}
		fullSnapshot := full && event.Type == "snapshot" && event.From == (coordinator.MapRevision{})
		if event.From != previous.Revision && !fullSnapshot {
			return empty, ErrResync
		}
	} else if event.From != (coordinator.MapRevision{}) {
		return empty, ErrResync
	}
	denied := value.Node.Status == "revoked"
	if v.profile(*value, now, denied) != nil {
		return empty, ErrMap
	}
	if parent.Err() != nil {
		return empty, parent.Err()
	}
	return Map{snapshot: *value, profileID: v.profileID, anchorID: v.anchorID, generation: v.generation, denied: denied, verified: sample}, nil
}

func sameSignature(a, b *coordinator.MapSignature) bool {
	return a.Version == b.Version && a.KeyID == b.KeyID && a.Algorithm == b.Algorithm && a.PayloadHash == b.PayloadHash && a.Signature == b.Signature && a.IssuedAt.Equal(b.IssuedAt) && a.ExpiresAt.Equal(b.ExpiresAt)
}

func identifier(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) })
}
func address(value string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(value)
	return a, err == nil && a.Is4() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast() && a.String() == value
}
func wireGuardKey(value string) bool {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != value {
		return false
	}
	var sum byte
	for _, b := range raw {
		sum |= b
	}
	return sum != 0
}

func (v *Verifier) profile(value coordinator.NetworkMapSnapshot, now time.Time, denied bool) error {
	n := value.Network
	if len(value.STUNEndpoints)+len(value.Relays) != 0 || value.RelayCredential != nil || n.IPv6CIDR != "" || len(n.DNS) != 0 || n.DNSConfig != nil || n.ClientPolicy != nil || len(n.Applications)+len(n.Services)+len(n.SharePeerGrants) != 0 || value.Node.AssignedIPv6 != "" || len(value.Node.AdvertisedIPs) != 0 || len(value.Peers) > v.config.MaxPeers {
		return ErrMap
	}
	prefix, err := netip.ParsePrefix(n.CIDR)
	local, ok := address(value.Node.AssignedIP)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() || !ok || !prefix.Contains(local) {
		return ErrMap
	}
	if denied {
		if len(value.Peers) != 0 {
			return ErrMap
		}
		return nil
	}
	if value.Node.ApprovalState != "approved" || value.Node.KeyExpiryEnabled && (value.Node.KeyExpiresAt == nil || value.Node.KeyExpiresAt.Before(value.MapSignature.ExpiresAt)) {
		return ErrMap
	}
	ids, keys, ips := map[string]bool{v.node: true}, map[string]bool{v.public: true}, map[string]bool{local.String(): true}
	rules := 0
	for _, peer := range value.Peers {
		if !identifier(peer.ID) || ids[peer.ID] || keys[peer.PublicKey] || !wireGuardKey(peer.PublicKey) || peer.NetworkID != v.network || !peer.ACLRestricted || len(peer.AllowedPorts) != 0 || len(peer.EndpointCandidates) != 0 || peer.Status == "revoked" || len(peer.AllowedIPs) != 1 {
			return ErrMap
		}
		peerPrefix, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || peerPrefix.Bits() != 32 || !prefix.Contains(peerPrefix.Addr()) {
			return ErrMap
		}
		peerAddress, ok := address(peerPrefix.Addr().String())
		if !ok || ips[peerAddress.String()] || peer.AllowedIPs[0] != peerAddress.String()+"/32" {
			return ErrMap
		}
		endpoint, err := netip.ParseAddrPort(peer.Endpoint)
		if err != nil || !endpoint.Addr().Is4() || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsLoopback() || endpoint.Addr().IsMulticast() || endpoint.Port() == 0 || endpoint.String() != peer.Endpoint || peer.EndpointGeneration == 0 || peer.EndpointExpiresAt == nil || !now.Before(*peer.EndpointExpiresAt) || peer.EndpointExpiresAt.Before(value.MapSignature.ExpiresAt) {
			return ErrMap
		}
		for _, direction := range []struct {
			grants      []coordinator.ACLGrant
			destination string
		}{{peer.ACLGrants, peerAddress.String() + "/32"}, {peer.IngressACLGrants, local.String() + "/32"}} {
			for _, grant := range direction.grants {
				if len(grant.DestinationCIDRs) != 1 || grant.DestinationCIDRs[0] != direction.destination || len(grant.AllowedPorts) == 0 {
					return ErrMap
				}
				for _, port := range grant.AllowedPorts {
					rules++
					if rules > v.config.MaxRules || port.Protocol != "tcp" && port.Protocol != "udp" || port.Port < 1 || port.Port > 65535 {
						return ErrMap
					}
				}
			}
		}
		ids[peer.ID], keys[peer.PublicKey], ips[peerAddress.String()] = true, true, true
	}
	return nil
}

// PersistCursor fences a late verified response against the latest durable
// intent before native apply. A failed/uncertain commit must not enable traffic.
func PersistCursor(ctx context.Context, store *state.Store, generation uint64, m Map) error {
	if store == nil || m.snapshot.MapSignature == nil || m.profileID == "" || generation == 0 || generation != m.generation {
		return ErrConfig
	}
	before, err := store.Load(ctx)
	if err != nil {
		return err
	}
	return store.Update(ctx, before.Revision, func(value *state.State) error {
		p, exists := value.Profiles[m.profileID]
		if !exists || p.Connection == nil || p.Connection.Intent != state.IntentConnected || p.Connection.Generation != generation || p.Trust.AnnouncementID != m.anchorID {
			return state.ErrStale
		}
		cursor := m.Cursor()
		p.Connection.MapCursor = &cursor
		value.Profiles[m.profileID] = p
		return nil
	})
}
