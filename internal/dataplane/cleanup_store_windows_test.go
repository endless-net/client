//go:build windows

package dataplane

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

// Test-only authenticated store codec/slot. Actual DPAPI/ACL is covered by the
// state owner; this exercises its real serialization, CAS and transition guards.
type cleanupCodec struct{ cipher.AEAD }

func (c cleanupCodec) Seal(data []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, data, nil), nil
}
func (c cleanupCodec) Open(data []byte) ([]byte, error) {
	if len(data) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], nil)
}

type cleanupSlot struct {
	mu                sync.Mutex
	data              []byte
	writes, loseACKAt int
}

func (s *cleanupSlot) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(s)
}
func (s *cleanupSlot) Read(limit int64) ([]byte, error) {
	if int64(len(s.data)) > limit {
		return nil, state.ErrCorrupt
	}
	return bytes.Clone(s.data), nil
}
func (s *cleanupSlot) Replace(data []byte) error {
	s.data = bytes.Clone(data)
	s.writes++
	if s.writes == s.loseACKAt {
		return state.ErrUncertain
	}
	return nil
}

func protectedCleanupStore(t *testing.T, journal *state.ResourceJournal) (*state.Store, *cleanupSlot, state.Profile) {
	return protectedCleanupStoreAt(t, journal, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), nil)
}

func protectedCleanupStoreAt(t *testing.T, journal *state.ResourceJournal, now time.Time, prepare func(*state.Profile)) (*state.Store, *cleanupSlot, state.Profile) {
	t.Helper()
	const installation = "753c9536-feb4-43a5-8d55-7a3c1b7e215d"
	keys, err := state.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: cleanupProfile, Owner: "S-1-5-21-1", ControlOrigin: "https://control.example", DeviceKeyHash: fingerprint}, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	attempt.Phase = enrollment.Registered
	attempt.AccountID, attempt.NetworkID, attempt.NodeID = "account", "network", "node"
	attempt.CredentialEnds = now.Add(time.Hour)
	connection := &state.ConnectionState{}
	for i, intent := range []state.DesiredIntent{state.IntentConnected, state.IntentDisconnected, state.IntentConnected} {
		if _, err := connection.AcceptIntent(intent, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	connection.Journal = journal
	profile := state.Profile{ID: cleanupProfile, ControlOrigin: attempt.ControlOrigin, Keys: keys, Attempt: &attempt, Connection: connection, Secrets: state.RemoteSecrets{RequestProof: "fixture-request-proof-00000000000000000", RegistrationID: attempt.RequestID, RegistrationRequest: "original-request", NodeCredential: "enc_original-credential"}}
	if prepare != nil {
		prepare(&profile)
	}
	value := state.State{Version: state.CurrentVersion, InstallationID: installation, Revision: 1, OwnerSID: attempt.Owner, ActiveProfileID: cleanupProfile, Profiles: map[string]state.Profile{cleanupProfile: profile}}
	if profile.Connection.Request != nil {
		value.Revision = profile.Connection.Request.Revision + 1
	}
	if err := value.Validate(installation); err != nil {
		t.Fatal("invalid registered fixture", err)
	}
	block, err := aes.NewCipher(bytes.Repeat([]byte{91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	codec := cleanupCodec{aead}
	plain, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	sealed, err := codec.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	slot := &cleanupSlot{data: sealed}
	store, err := state.New(slot, codec, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	return store, slot, profile
}

func TestWindowsCleanupRealProtectedStoreTransitionsAndLostACKRecovery(t *testing.T) {
	for lostAt := 0; lostAt <= 6; lostAt++ {
		c, memory, _, guards := cleanupFixture(t)
		store, slot, original := protectedCleanupStore(t, memory.value.Profiles[cleanupProfile].Connection.Journal)
		c.store = store
		slot.loseACKAt = lostAt
		ctx := cleanupContext(t)
		err := c.Run(ctx, cleanupProfile, policySession, 1)
		if lostAt == 0 && err != nil || lostAt != 0 && !errors.Is(err, state.ErrUncertain) {
			t.Fatal("real protected transition/ACK failure", lostAt, err)
		}
		slot.loseACKAt = 0
		if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil {
			t.Fatal("real store recovery failed", lostAt, err)
		}
		value, err := store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p := value.Profiles[cleanupProfile]
		if p.Connection.Journal != nil || p.Connection.Generation != 3 || p.Connection.Intent != state.IntentConnected || p.Secrets != original.Secrets || !bytes.Equal(p.Keys.WireGuardPrivate, original.Keys.WireGuardPrivate) || !bytes.Equal(p.Keys.IdentityPrivate, original.Keys.IdentityPrivate) || len(guards.live) != 0 {
			t.Fatal("cleanup changed original registration/keys/intent or retained effects")
		}
	}
}
