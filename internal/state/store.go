// Package state owns the protected Client installation state. No value from
// this package is an IPC projection: keys and remote grants stay in the daemon.
package state

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/endless-net/client/internal/enrollment"
)

var (
	ErrMissing   = errors.New("client state not initialized")
	ErrCorrupt   = errors.New("client state invalid or inaccessible")
	ErrStale     = errors.New("client state revision changed")
	ErrUncertain = errors.New("client state write outcome uncertain; reload required")
	uuid         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type Keys struct{ IdentityPrivate, WireGuardPrivate []byte }

func GenerateKeys() (Keys, error) {
	_, identity, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	wg, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	return Keys{IdentityPrivate: identity, WireGuardPrivate: wg.Bytes()}, nil
}

func (k Keys) Fingerprint() ([32]byte, error) {
	if len(k.IdentityPrivate) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.NewKeyFromSeed(k.IdentityPrivate[:ed25519.SeedSize]), k.IdentityPrivate) || len(k.WireGuardPrivate) != 32 || bytes.Equal(k.WireGuardPrivate, make([]byte, 32)) {
		return [32]byte{}, ErrCorrupt
	}
	if _, err := ecdh.X25519().NewPrivateKey(k.WireGuardPrivate); err != nil {
		return [32]byte{}, ErrCorrupt
	}
	return sha256.Sum256(k.IdentityPrivate[ed25519.SeedSize:]), nil
}

type RemoteSecrets struct {
	RequestProof, PollProof, TransactionID             string
	Access, Renewal, RegistrationID, RegistrationProof string
	SessionID, UserID                                  string
	SessionExpiresAt, RenewalExpiresAt                 time.Time
	RegistrationRequest, NodeCredential                string
}

type RegistrationProgress struct {
	NextCallAt    time.Time
	BlockedReason string
}

type LoginProgress struct {
	CreatedAt, ExpiresAt, ReplayUntil, NextPollAt time.Time
	VerificationURL, BlockedReason                string
}

type ServerTrust struct {
	KeyID, AnnouncementID string
	Bundle                []byte
}

type Profile struct {
	ID, DisplayName, ControlOrigin string
	Keys                           Keys
	Attempt                        *enrollment.Attempt
	Secrets                        RemoteSecrets
	ActiveSession                  *ActiveSession        `json:",omitempty"`
	ActiveNodeCredential           *ActiveNodeCredential `json:",omitempty"`
	SessionRefresh                 *SessionRefresh       `json:",omitempty"`
	NodeRenewal                    *NodeRenewal          `json:",omitempty"`
	Trust                          ServerTrust
	Login                          LoginProgress
	Registration                   RegistrationProgress
	DefaultTarget                  *DefaultTargetProgress      `json:",omitempty"`
	Approval                       *ApprovalProgress           `json:",omitempty"`
	Connection                     *ConnectionState            `json:",omitempty"`
	RemoteRevocations              map[string]RemoteRevocation `json:",omitempty"`
	// Persisted by the worker before a remote mutation; cancellation must not
	// imply rollback of a call whose response is still unresolved.
	RemoteInFlight bool
	InFlightAt     time.Time
	// Exact caller inputs are persisted for changed-input conflict detection.
	Mode     int32
	Hostname string
}

type BootstrapCheckpoint struct {
	Revision uint64
	Digest   [32]byte
}

type State struct {
	Version              uint32
	BootstrapCheckpoints map[string]BootstrapCheckpoint `json:",omitempty"`
	// Stable local installation binding, distinct from the per-start IPC
	// RuntimeInfo.instance_id. Restart never reseals keys under a new runtime ID.
	InstallationID  string
	Revision        uint64
	OwnerSID        string
	ActiveProfileID string
	Profiles        map[string]Profile
	TransportHints  []TransportHint `json:",omitempty"`
	HintObservedAt  time.Time       `json:",omitempty"`
}

const CurrentVersion uint32 = 7

func (s State) Validate(installation string) error {
	if s.Version != CurrentVersion || !validID(s.InstallationID) || s.InstallationID != installation || s.Revision == 0 || (len(s.Profiles) == 0) != (s.OwnerSID == "") {
		return ErrCorrupt
	}
	if len(s.Profiles) == 0 && s.ActiveProfileID != "" {
		return ErrCorrupt
	}
	if len(s.Profiles) > 0 {
		if _, ok := s.Profiles[s.ActiveProfileID]; !ok {
			return ErrCorrupt
		}
	}
	for origin, checkpoint := range s.BootstrapCheckpoints {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Host != strings.ToLower(u.Host) || u.String() != origin || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || checkpoint.Revision == 0 || checkpoint.Digest == [32]byte{} {
			return ErrCorrupt
		}
	}
	if validateTransportHints(s) != nil {
		return ErrCorrupt
	}
	for id, p := range s.Profiles {
		if validateActiveCredentials(p) != nil {
			return ErrCorrupt
		}
		if validateCredentialProgress(p) != nil {
			return ErrCorrupt
		}
		if validateDefaultTarget(p) != nil {
			return ErrCorrupt
		}
		if validateRevocations(s.OwnerSID, p) != nil {
			return ErrCorrupt
		}
		if validateConnection(p) != nil {
			return ErrCorrupt
		}
		if p.Connection != nil && p.Connection.Request != nil && p.Connection.Request.Revision >= s.Revision {
			return ErrCorrupt
		}
		u, err := url.Parse(p.ControlOrigin)
		if !validID(id) || id != p.ID || err != nil || u.Scheme != "https" || u.Host == "" || u.Host != strings.ToLower(u.Host) || u.String() != p.ControlOrigin || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return ErrCorrupt
		}
		if len(p.Trust.Bundle) == 0 {
			if p.Trust.KeyID != "" || p.Trust.AnnouncementID != "" {
				return ErrCorrupt
			}
		} else {
			digest := sha256.Sum256(append([]byte(p.ControlOrigin+"\x00"), p.Trust.Bundle...))
			if p.Trust.KeyID == "" || p.Trust.AnnouncementID != fmt.Sprintf("sha256:%x", digest) {
				return ErrCorrupt
			}
		}
		if p.Attempt == nil {
			if len(p.Keys.IdentityPrivate) != 0 || len(p.Keys.WireGuardPrivate) != 0 || p.Secrets != (RemoteSecrets{}) || p.RemoteInFlight || !p.InFlightAt.IsZero() || p.Login != (LoginProgress{}) || p.Registration != (RegistrationProgress{}) || p.Approval != nil {
				return ErrCorrupt
			}
			continue
		}
		if p.RemoteInFlight != !p.InFlightAt.IsZero() {
			return ErrCorrupt
		}
		if p.RemoteInFlight && (p.InFlightAt.Before(p.Attempt.CreatedAt) || p.InFlightAt.After(p.Attempt.ObservedAt)) {
			return ErrCorrupt
		}
		if p.Secrets.TransactionID != "" {
			if !validID(p.Secrets.TransactionID) || len(p.Secrets.PollProof) < 32 || p.Login.CreatedAt.IsZero() || !p.Login.ExpiresAt.Equal(p.Login.CreatedAt.Add(enrollment.LoginTTL)) || p.Login.ReplayUntil.Before(p.Login.ExpiresAt.Add(720*time.Second)) {
				return ErrCorrupt
			}
			if p.Login.VerificationURL != "" && p.Login.VerificationURL != enrollment.NativeConsentOrigin(p.ControlOrigin)+"/auth/native-login/"+p.Secrets.TransactionID && p.Login.VerificationURL != enrollment.NativeConsentOrigin(p.ControlOrigin)+"/api/v1/auth/native-login/"+p.Secrets.TransactionID {
				return ErrCorrupt
			}
		} else if p.Secrets.PollProof != "" || !p.Login.CreatedAt.IsZero() || !p.Login.ExpiresAt.IsZero() || p.Login.VerificationURL != "" {
			return ErrCorrupt
		}
		fingerprint, err := p.Keys.Fingerprint()
		if err != nil || p.Attempt.Validate() != nil || p.Attempt.Owner != s.OwnerSID || p.Attempt.ProfileID != id || p.Attempt.ControlOrigin != p.ControlOrigin || p.Attempt.DeviceKeyHash != fingerprint || len(p.Secrets.RequestProof) < 32 {
			return ErrCorrupt
		}
		if p.Secrets.RegistrationRequest != "" && (!validID(p.Secrets.RegistrationID) || !p.Attempt.BrowserApproval && (p.Attempt.AccountID == "" || p.Attempt.NetworkID == "")) {
			return ErrCorrupt
		}
		if validateApproval(p) != nil {
			return ErrCorrupt
		}
		if p.Secrets.NodeCredential != "" && (p.Attempt.Phase != enrollment.Registered || p.Secrets.RegistrationRequest == "") {
			return ErrCorrupt
		}
		if p.Attempt.Phase == enrollment.Registered && p.Secrets.RegistrationRequest != "" && p.Secrets.NodeCredential == "" {
			return ErrCorrupt
		}
	}
	return nil
}

func validID(id string) bool {
	return uuid.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

// Protector is implemented by per-service-user DPAPI on Windows. Test codecs
// exist only in tests; there is no production plaintext or machine-wide fallback.
type Protector interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}
type Slot interface {
	Read(int64) ([]byte, error)
	Replace([]byte) error
}
type Backend interface {
	Exclusive(context.Context, func(Slot) error) error
}

type Store struct {
	backend             Backend
	protector           Protector
	installation        string
	maxPlain, maxCipher int64
}

func New(backend Backend, protector Protector, installation string, maxPlain, maxCipher int64) (*Store, error) {
	if backend == nil || protector == nil || !validID(installation) || maxPlain <= 0 || maxCipher <= maxPlain || maxCipher >= int64(^uint(0)>>1) {
		return nil, ErrCorrupt
	}
	return &Store{backend: backend, protector: protector, installation: installation, maxPlain: maxPlain, maxCipher: maxCipher}, nil
}

func (s *Store) decode(slot Slot) (State, error) {
	data, err := slot.Read(s.maxCipher)
	if err != nil {
		return State{}, err
	}
	if len(data) == 0 || int64(len(data)) > s.maxCipher {
		return State{}, ErrCorrupt
	}
	plain, err := s.protector.Open(data)
	if err != nil || int64(len(plain)) > s.maxPlain {
		return State{}, ErrCorrupt
	}
	defer clear(plain)
	var state State
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if err = d.Decode(&state); err != nil {
		return State{}, ErrCorrupt
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return State{}, ErrCorrupt
	}
	return state, nil
}

func (s *Store) read(slot Slot) (State, error) {
	state, err := s.decode(slot)
	if err != nil {
		return State{}, err
	}
	if state.Validate(s.installation) != nil {
		return State{}, ErrCorrupt
	}
	return state, nil
}

func (s *Store) save(ctx context.Context, slot Slot, state State) error {
	if state.Validate(s.installation) != nil {
		return ErrCorrupt
	}
	plain, err := json.Marshal(state)
	if err != nil || int64(len(plain)) > s.maxPlain {
		return ErrCorrupt
	}
	defer clear(plain)
	cipher, err := s.protector.Seal(plain)
	if err != nil || int64(len(cipher)) > s.maxCipher {
		return ErrCorrupt
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = slot.Replace(cipher); err != nil {
		return ErrUncertain
	}
	return nil
}

// Initialize is an explicit installation action. Startup performs the direct
// Upgrade then Load; neither initializes missing or repairs corrupt state.
func (s *Store) Initialize(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.backend.Exclusive(ctx, func(slot Slot) error {
		_, err := slot.Read(s.maxCipher)
		if !errors.Is(err, ErrMissing) {
			if err == nil {
				return ErrStale
			}
			return err
		}
		return s.save(ctx, slot, State{Version: CurrentVersion, InstallationID: s.installation, Revision: 1, Profiles: map[string]Profile{}})
	})
}

func (s *Store) Load(ctx context.Context) (state State, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = s.backend.Exclusive(ctx, func(slot Slot) error { var e error; state, e = s.read(slot); return e })
	return state, err
}

func (s *Store) Owner(ctx context.Context) (string, error) {
	state, err := s.Load(ctx)
	return state.OwnerSID, err
}

// Update commits one revision before the caller performs network effects.
// The callback performs only local state changes. OS caller authorization and
// initial owner claim must be checked inside this same callback by the handler.
func (s *Store) Update(ctx context.Context, revision uint64, change func(*State) error) error {
	if change == nil {
		return ErrCorrupt
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.backend.Exclusive(ctx, func(slot Slot) error {
		before, err := s.read(slot)
		if err != nil {
			return err
		}
		if before.Revision != revision || revision == ^uint64(0) {
			return ErrStale
		}
		// Decode independently so callbacks cannot mutate the validation baseline.
		after, err := s.read(slot)
		if err != nil {
			return err
		}
		if err = change(&after); err != nil {
			return err
		}
		if after.Version != before.Version || after.InstallationID != before.InstallationID || after.Revision != before.Revision || (before.OwnerSID != "" && after.OwnerSID != before.OwnerSID) {
			return ErrCorrupt
		}
		for origin, previous := range before.BootstrapCheckpoints {
			next := after.BootstrapCheckpoints[origin]
			if next.Revision < previous.Revision || next.Revision == previous.Revision && next.Digest != previous.Digest {
				return ErrCorrupt
			}
		}
		if after.HintObservedAt.Before(before.HintObservedAt) {
			return ErrCorrupt
		}
		for id, p := range before.Profiles {
			next, ok := after.Profiles[id]
			if !ok || p.ControlOrigin != next.ControlOrigin {
				return ErrCorrupt
			}
			if activeCredentialsUpdate(p, next) != nil {
				return ErrCorrupt
			}
			if credentialProgressUpdate(p, next) != nil {
				return ErrCorrupt
			}
			if revocationsUpdate(p, next) != nil {
				return ErrCorrupt
			}
			if defaultTargetUpdate(p, next) != nil {
				return ErrCorrupt
			}
			if connectionUpdate(p.Connection, next.Connection) != nil || !connectionRequestRevision(p.Connection, next.Connection, before.Revision) {
				return ErrCorrupt
			}
			if p.Attempt != nil && (next.Attempt == nil || p.Attempt.Binding != next.Attempt.Binding || !bytes.Equal(p.Keys.IdentityPrivate, next.Keys.IdentityPrivate) || !bytes.Equal(p.Keys.WireGuardPrivate, next.Keys.WireGuardPrivate) || p.Secrets.RequestProof != next.Secrets.RequestProof || p.Mode != next.Mode || p.Hostname != next.Hostname) {
				return ErrCorrupt
			}
			if p.Attempt != nil {
				a, n := p.Attempt, next.Attempt
				if approvalUpdate(p, next) != nil {
					return ErrCorrupt
				}
				if !a.CreatedAt.Equal(n.CreatedAt) || !a.LoginUntil.Equal(n.LoginUntil) || !a.Deadline.Equal(n.Deadline) || (!a.ApprovalUntil.IsZero() && !a.ApprovalUntil.Equal(n.ApprovalUntil)) || (!a.RecoveryUntil.IsZero() && !a.RecoveryUntil.Equal(n.RecoveryUntil)) || n.ObservedAt.Before(a.ObservedAt) || n.Revision < a.Revision || (a.AccountID != "" && (a.AccountID != n.AccountID || a.NetworkID != n.NetworkID)) || (a.NodeID != "" && a.NodeID != n.NodeID) {
					return ErrCorrupt
				}
				if (p.Secrets.PollProof != "" && p.Secrets.PollProof != next.Secrets.PollProof) || (p.Secrets.TransactionID != "" && p.Secrets.TransactionID != next.Secrets.TransactionID) || (p.Secrets.RegistrationID != "" && p.Secrets.RegistrationID != next.Secrets.RegistrationID) {
					return ErrCorrupt
				}
				if (p.Secrets.RegistrationRequest != "" && p.Secrets.RegistrationRequest != next.Secrets.RegistrationRequest) || (p.Secrets.NodeCredential != "" && p.Secrets.NodeCredential != next.Secrets.NodeCredential) {
					return ErrCorrupt
				}
				if p.Secrets.RegistrationRequest != "" && p.Secrets.Access != next.Secrets.Access {
					return ErrCorrupt
				}
				if !p.Login.CreatedAt.IsZero() && (!p.Login.CreatedAt.Equal(next.Login.CreatedAt) || !p.Login.ExpiresAt.Equal(next.Login.ExpiresAt) || !p.Login.ReplayUntil.Equal(next.Login.ReplayUntil)) {
					return ErrCorrupt
				}
			}
		}
		for id, p := range after.Profiles {
			if _, exists := before.Profiles[id]; !exists && (connectionUpdate(nil, p.Connection) != nil || !connectionRequestRevision(nil, p.Connection, before.Revision)) {
				return ErrCorrupt
			}
		}
		after.Revision++
		return s.save(ctx, slot, after)
	})
}

// AcceptBootstrap verifies against the latest per-origin high-water while holding
// the protected backend lock. The callback performs only local cryptography.
// No bundles may be used unless this returns nil, including after a lost ACK.
func (s *Store) AcceptBootstrap(parent context.Context, origin string, verify func(BootstrapCheckpoint) (BootstrapCheckpoint, error)) error {
	if verify == nil {
		return ErrCorrupt
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return s.backend.Exclusive(ctx, func(slot Slot) error {
		current, err := s.read(slot)
		if err != nil {
			return err
		}
		previous := current.BootstrapCheckpoints[origin]
		next, err := verify(previous)
		if err != nil {
			return err
		}
		if next.Revision == 0 || next.Digest == [32]byte{} || next.Revision < previous.Revision || next.Revision == previous.Revision && next.Digest != previous.Digest {
			return ErrCorrupt
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if next == previous {
			return nil
		}
		if current.Revision == ^uint64(0) {
			return ErrStale
		}
		if current.BootstrapCheckpoints == nil {
			current.BootstrapCheckpoints = make(map[string]BootstrapCheckpoint)
		}
		current.BootstrapCheckpoints[origin] = next
		current.Revision++
		return s.save(ctx, slot, current)
	})
}
