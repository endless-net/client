package state

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/enrollment"
)

const (
	MaxTransportHints = 256
	TransportHintTTL  = 24 * time.Hour
)

// TransportHintKey is private installation metadata, not a transport profile or
// authority. ContextDigest hashes OS network identities; it is never an SSID/IP.
// Keys/parameters that can decrypt or construct a profile are not stored here.
type TransportHintKey struct {
	LocalProfileID, AccountID, NetworkID string
	LocalNodeID, PeerNodeID              string
	LocalPublicKey, PeerPublicKey        string
	ContextDigest                        string
	Engine, ProtocolVersion              string
	TransportProfileID, ProfileDigest    string
	ProfileVersion                       uint32
}

type TransportHint struct {
	Key                    TransportHintKey
	SucceededAt, ExpiresAt time.Time
	RTT, StableSpan        time.Duration
	SuccessfulProbes       uint32
}

func (TransportHintKey) String() string   { return "[private transport hint binding]" }
func (TransportHintKey) GoString() string { return "[private transport hint binding]" }
func (TransportHint) String() string      { return "[private transport success hint]" }
func (TransportHint) GoString() string    { return "[private transport success hint]" }

func hintIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func hintPublicKey(value string) bool {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32 && !bytes.Equal(raw, make([]byte, 32)) && base64.StdEncoding.EncodeToString(raw) == value
}

func (k TransportHintKey) validate(s State) error {
	p, ok := s.Profiles[k.LocalProfileID]
	if !ok || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Attempt.AccountID != k.AccountID || p.Attempt.NetworkID != k.NetworkID || p.Attempt.NodeID != k.LocalNodeID {
		return ErrCorrupt
	}
	for _, id := range []string{k.AccountID, k.NetworkID, k.LocalNodeID, k.PeerNodeID, k.TransportProfileID} {
		if !hintIdentifier(id) {
			return ErrCorrupt
		}
	}
	supported := k.Engine == transport.WireGuard && k.ProtocolVersion == "1" || k.Engine == transport.AmneziaWG && k.ProtocolVersion == "3.1"
	if k.LocalNodeID == k.PeerNodeID || k.LocalPublicKey == k.PeerPublicKey || !hintPublicKey(k.LocalPublicKey) || !hintPublicKey(k.PeerPublicKey) || !transport.ValidDigest(k.ContextDigest) || !transport.ValidDigest(k.ProfileDigest) || k.ProfileVersion != transport.Version || !supported {
		return ErrCorrupt
	}
	key, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil || base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()) != k.LocalPublicKey {
		return ErrCorrupt
	}
	return nil
}

func (h TransportHint) validate(s State) error {
	if h.Key.validate(s) != nil || h.SucceededAt.IsZero() || !h.ExpiresAt.Equal(h.SucceededAt.Add(TransportHintTTL)) || h.SuccessfulProbes < 3 || h.StableSpan < 5*time.Second || h.StableSpan > 30*time.Second || h.RTT < 0 || h.RTT > 3*time.Second {
		return ErrCorrupt
	}
	return nil
}

func validateTransportHints(s State) error {
	if len(s.TransportHints) > MaxTransportHints || len(s.TransportHints) > 0 && s.HintObservedAt.IsZero() {
		return ErrCorrupt
	}
	seen := make(map[TransportHintKey]bool, len(s.TransportHints))
	for _, h := range s.TransportHints {
		if h.validate(s) != nil || h.SucceededAt.After(s.HintObservedAt) || seen[h.Key] {
			return ErrCorrupt
		}
		seen[h.Key] = true
	}
	return nil
}

// expireTransportHints also advances the durable wall watermark. Rollback
// discards priority and keeps the previous watermark until the clock catches up.
// Call only within Store.Update, together with the resulting selection metadata.
func (s *State) expireTransportHints(now time.Time) error {
	if now.IsZero() {
		return ErrCorrupt
	}
	if now.Before(s.HintObservedAt) {
		s.TransportHints = nil
		return nil
	}
	s.HintObservedAt = now
	kept := s.TransportHints[:0]
	for _, h := range s.TransportHints {
		if !now.Before(h.SucceededAt) && now.Before(h.ExpiresAt) && h.validate(*s) == nil {
			kept = append(kept, h)
		}
	}
	s.TransportHints = kept
	return nil
}

// RecordTransportSuccess must be called within Store.Update by the supervisor
// after fresh own Direct qualification and bilateral activation. This method
// checks durable metadata, never the cryptographic proof or application rights.
func (s *State) RecordTransportSuccess(h TransportHint, now time.Time) error {
	if h.validate(*s) != nil || now.Before(h.SucceededAt) || !now.Before(h.ExpiresAt) || now.Before(s.HintObservedAt) {
		return ErrCorrupt
	}
	if err := s.expireTransportHints(now); err != nil {
		return err
	}
	for i, old := range s.TransportHints {
		if old.Key == h.Key {
			if h.SucceededAt.Before(old.SucceededAt) || h.SucceededAt.Equal(old.SucceededAt) && (h.RTT != old.RTT || h.StableSpan != old.StableSpan || h.SuccessfulProbes != old.SuccessfulProbes) {
				return ErrStale
			}
			s.TransportHints[i] = h
			return nil
		}
	}
	if len(s.TransportHints) == MaxTransportHints {
		oldest := 0
		for i, h := range s.TransportHints {
			if h.SucceededAt.Before(s.TransportHints[oldest].SucceededAt) {
				oldest = i
			}
		}
		if h.SucceededAt.Before(s.TransportHints[oldest].SucceededAt) {
			return nil
		}
		s.TransportHints = append(s.TransportHints[:oldest], s.TransportHints[oldest+1:]...)
	}
	s.TransportHints = append(s.TransportHints, h)
	return nil
}

// ReconcileTransportHints receives ALL current permitted pair/profile bindings
// of one local profile, derived from a fresh signed map and known OS context.
// Unknown context (empty digest/current set) removes that profile's priorities.
// Returned keys are hints only: every choice still requires new path probes.
// Reads are committed with Store.Update so expired hints cannot revive on restart.
func (s *State) ReconcileTransportHints(profile, contextDigest string, current []TransportHintKey, now time.Time) ([]TransportHintKey, error) {
	if _, ok := s.Profiles[profile]; !ok {
		return nil, ErrStale
	}
	allowed := make(map[TransportHintKey]bool, len(current))
	if contextDigest != "" && !transport.ValidDigest(contextDigest) {
		return nil, ErrCorrupt
	}
	for _, k := range current {
		if k.LocalProfileID != profile || k.ContextDigest != contextDigest || k.validate(*s) != nil {
			return nil, ErrCorrupt
		}
		allowed[k] = true
	}
	if err := s.expireTransportHints(now); err != nil {
		return nil, err
	}
	kept := s.TransportHints[:0]
	var preferred []TransportHint
	for _, h := range s.TransportHints {
		if h.Key.LocalProfileID != profile {
			kept = append(kept, h)
		} else if allowed[h.Key] {
			kept = append(kept, h)
			preferred = append(preferred, h)
		}
	}
	s.TransportHints = kept
	sort.SliceStable(preferred, func(i, j int) bool {
		if !preferred[i].SucceededAt.Equal(preferred[j].SucceededAt) {
			return preferred[i].SucceededAt.After(preferred[j].SucceededAt)
		}
		return preferred[i].RTT < preferred[j].RTT
	})
	result := make([]TransportHintKey, len(preferred))
	for i, h := range preferred {
		result[i] = h.Key
	}
	return result, nil
}

// ForgetTransportHint removes priority after a new failed probe. It does not
// change the current route, access lease, remote configuration or key material.
func (s *State) ForgetTransportHint(key TransportHintKey) {
	kept := s.TransportHints[:0]
	for _, h := range s.TransportHints {
		if h.Key != key {
			kept = append(kept, h)
		}
	}
	s.TransportHints = kept
}
