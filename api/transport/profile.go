// Package transport defines Client-owned declarative peer transport profiles.
// Validation establishes format and resource bounds, never current authority,
// signature trust, reachability or permission to execute a profile.
package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Version          = 1
	MaxProfiles      = 8
	MaxProfileBytes  = 64 * 1024
	MaxPrelude       = 16
	MaxDatagramBytes = 1200
	MaxProbeBytes    = 64 * 1024
	MaxProbeMillis   = 3000
	WireGuard        = "wireguard"
	AmneziaWG        = "amneziawg"
)

var (
	ErrInvalid     = errors.New("invalid transport configuration")
	ErrUnsupported = errors.New("unsupported transport capability")
)

// Profile has no endpoint, script, raw UAPI or crypto implementation. Its
// authority and pair binding must come from the verified Coordinator envelope.
// Header protection material and literal templates must stay out of diagnostics.
type Profile struct {
	Version         uint32     `json:"version"`
	ID              string     `json:"id"`
	Revision        uint64     `json:"revision"`
	Engine          string     `json:"engine"`
	ProtocolVersion string     `json:"protocol_version"`
	Prelude         []Datagram `json:"prelude"`
	AWG             *AWG       `json:"awg,omitempty"`
}

func (Profile) String() string   { return "[transport profile]" }
func (Profile) GoString() string { return "[transport profile]" }

type Range struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

// AWG is a bounded whitelist, not an arbitrary upstream configuration. Crypto
// rekey/reject lifetimes are kept at the pinned engine's defaults. Header ranges
// and padding have fixed H1..H4/S1..S4 order. All fields are explicit on the wire.
type AWG struct {
	Headers             []Range  `json:"headers"`
	Padding             []uint16 `json:"padding"`
	HeaderProtectionKey []byte   `json:"header_protection_key"`
	ContentPadding      Range    `json:"content_padding"`
	RandomTrailers      bool     `json:"random_trailers"`
	DisableCookies      bool     `json:"disable_cookies"`
}

func (AWG) String() string   { return "[AWG parameters]" }
func (AWG) GoString() string { return "[AWG parameters]" }

// Datagram is sent only to the candidate's authorized endpoint. Delay is a
// bounded part of the original probe budget, not a timer that extends it.
type Datagram struct {
	DelayMS  uint16    `json:"delay_ms"`
	Segments []Segment `json:"segments"`
}

// Segment selects exactly one primitive: literal bytes or CSPRNG byte count.
// There is no interpreter for arbitrary tags, loops or downloaded executable code.
type Segment struct {
	Bytes  []byte `json:"bytes,omitempty"`
	Random uint16 `json:"random,omitempty"`
}

func (Datagram) String() string   { return "[transport datagram template]" }
func (Datagram) GoString() string { return "[transport datagram template]" }
func (Segment) String() string    { return "[transport segment]" }
func (Segment) GoString() string  { return "[transport segment]" }

func identifier(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})
}

func supported(engine, protocol string) bool {
	return engine == WireGuard && protocol == "1" || engine == AmneziaWG && protocol == "3.1"
}

func validateProfile(p Profile) error {
	if p.Version != Version || !supported(p.Engine, p.ProtocolVersion) {
		return ErrUnsupported
	}
	if !identifier(p.ID) || p.Revision == 0 || p.Prelude == nil || len(p.Prelude) > MaxPrelude {
		return ErrInvalid
	}
	if p.Engine == WireGuard && p.AWG != nil || p.Engine == AmneziaWG && !validAWG(p.AWG) {
		return ErrInvalid
	}
	delay := 0
	for _, packet := range p.Prelude {
		if len(packet.Segments) == 0 || len(packet.Segments) > MaxDatagramBytes {
			return ErrInvalid
		}
		delay += int(packet.DelayMS)
		if delay > MaxProbeMillis {
			return ErrInvalid
		}
		size := 0
		for _, segment := range packet.Segments {
			if (len(segment.Bytes) == 0) == (segment.Random == 0) || len(segment.Bytes) > MaxDatagramBytes || segment.Random > MaxDatagramBytes {
				return ErrInvalid
			}
			size += len(segment.Bytes) + int(segment.Random)
			if size > MaxDatagramBytes {
				return ErrInvalid
			}
		}
	}
	return nil
}

func validAWG(a *AWG) bool {
	if a == nil || len(a.Headers) != 4 || len(a.Padding) != 4 || len(a.HeaderProtectionKey) != 32 || a.ContentPadding.Min > a.ContentPadding.Max || a.ContentPadding.Max > MaxDatagramBytes {
		return false
	}
	nonzero := false
	for _, b := range a.HeaderProtectionKey {
		nonzero = nonzero || b != 0
	}
	if !nonzero {
		return false
	}
	for i, h := range a.Headers {
		if h.Min > h.Max || a.Padding[i] < 12 || a.Padding[i] > MaxDatagramBytes {
			return false
		}
		for j := 0; j < i; j++ {
			other := a.Headers[j]
			if h.Min <= other.Max && other.Min <= h.Max {
				return false
			}
		}
	}
	return true
}

func DecodeProfile(raw []byte) (Profile, error) {
	var p Profile
	if decode(raw, MaxProfileBytes, &p) != nil {
		return Profile{}, ErrInvalid
	}
	if err := ValidateProfile(p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// CanonicalProfile makes the digest independent of JSON whitespace, field order
// and escaping. Ordered prelude segments and H1..H4 are deliberately preserved.
func CanonicalProfile(p Profile) ([]byte, error) {
	if err := validateProfile(p); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > MaxProfileBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}

func ValidateProfile(p Profile) error {
	_, err := CanonicalProfile(p)
	return err
}

// ValidateProfiles bounds one peer's set and rejects ambiguous duplicate IDs.
// Empty means no configured candidates, never a default or implicit permission.
func ValidateProfiles(profiles []Profile) error {
	if len(profiles) > MaxProfiles {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		if err := ValidateProfile(p); err != nil {
			return err
		}
		if seen[p.ID] {
			return ErrInvalid
		}
		seen[p.ID] = true
	}
	return nil
}

func ProfileDigest(p Profile) (string, error) {
	raw, err := CanonicalProfile(p)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func ValidDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
}
