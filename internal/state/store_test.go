package state

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
)

const installation = "753c9536-feb4-43a5-8d55-7a3c1b7e215d"
const profileID = "862e4ef0-fe90-4b89-bbbd-6e5f67bf8c56"

type memoryBackend struct {
	mu                    sync.Mutex
	data                  []byte
	failBefore, failAfter bool
}

func (b *memoryBackend) Exclusive(ctx context.Context, fn func(Slot) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fn(b)
}
func (b *memoryBackend) Read(limit int64) ([]byte, error) {
	if b.data == nil {
		return nil, ErrMissing
	}
	if int64(len(b.data)) > limit {
		return nil, ErrCorrupt
	}
	return append([]byte(nil), b.data...), nil
}
func (b *memoryBackend) Replace(data []byte) error {
	if b.failBefore {
		return ErrCorrupt
	}
	b.data = append([]byte(nil), data...)
	if b.failAfter {
		return ErrUncertain
	}
	return nil
}

// Test-only authenticated codec. No fake or plaintext codec ships in production.
type testCodec struct{ cipher.AEAD }

func (c testCodec) Seal(data []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, data, nil), nil
}
func (c testCodec) Open(data []byte) ([]byte, error) {
	if len(data) < c.NonceSize() {
		return nil, ErrCorrupt
	}
	return c.AEAD.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], nil)
}
func codec(t *testing.T) testCodec {
	t.Helper()
	block, err := aes.NewCipher(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return testCodec{a}
}

func fixture(t *testing.T) (*Store, *memoryBackend) {
	t.Helper()
	b := &memoryBackend{}
	s, err := New(b, codec(t), installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, b
}

func addAttempt(s *State) error {
	keys, err := GenerateKeys()
	if err != nil {
		return err
	}
	fingerprint, err := keys.Fingerprint()
	if err != nil {
		return err
	}
	a, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: profileID, Owner: "S-1-5-21-1", ControlOrigin: "https://control.example", DeviceKeyHash: fingerprint}, time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	s.OwnerSID = a.Owner
	s.ActiveProfileID = profileID
	s.Profiles[profileID] = Profile{ID: profileID, ControlOrigin: a.ControlOrigin, Keys: keys, Attempt: &a, Secrets: RemoteSecrets{RequestProof: "fixture-request-proof-00000000000000000"}}
	return nil
}

func TestAtomicStateRecoveryAndBinding(t *testing.T) {
	s, b := fixture(t)
	ctx := context.Background()
	if err := s.Update(ctx, 1, addAttempt); err != nil {
		t.Fatal(err)
	}
	original, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A new Store installation has no process-local key/UUID fallback.
	restored, err := New(b, codec(t), installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restored.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Profiles[profileID].Keys.IdentityPrivate, original.Profiles[profileID].Keys.IdentityPrivate) || *loaded.Profiles[profileID].Attempt != *original.Profiles[profileID].Attempt {
		t.Fatal("restart changed identity/deadlines")
	}
	if bytes.Contains(b.data, []byte(original.Profiles[profileID].Secrets.RequestProof)) {
		t.Fatal("plaintext grant on storage")
	}
	if err = s.Update(ctx, 1, func(*State) error { return nil }); !errors.Is(err, ErrStale) {
		t.Fatal("stale revision accepted")
	}
	for _, change := range []func(*State) error{
		func(s *State) error {
			p := s.Profiles[profileID]
			p.Attempt.CreatedAt = p.Attempt.CreatedAt.Add(time.Minute)
			p.Attempt.ObservedAt = p.Attempt.CreatedAt
			p.Attempt.LoginUntil = p.Attempt.LoginUntil.Add(time.Minute)
			p.Attempt.Deadline = p.Attempt.Deadline.Add(time.Minute)
			s.Profiles[profileID] = p
			return nil
		},
		func(s *State) error { s.OwnerSID = "S-1-5-21-2"; return nil },
		func(s *State) error {
			p := s.Profiles[profileID]
			p.Attempt.RequestID = "20eb7521-41a9-47f1-ab43-a6ce75bdb46b"
			s.Profiles[profileID] = p
			return nil
		},
		func(s *State) error {
			p := s.Profiles[profileID]
			p.Secrets.RequestProof = "changed-request-proof-0000000000000"
			s.Profiles[profileID] = p
			return nil
		},
	} {
		if err = s.Update(ctx, 2, change); !errors.Is(err, ErrCorrupt) {
			t.Fatal("immutable binding replaced")
		}
	}
	// A pre-commit failure retains all original bytes.
	snapshot := append([]byte(nil), b.data...)
	b.failBefore = true
	if err = s.Update(ctx, 2, func(*State) error { return nil }); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot, b.data) {
		t.Fatal("failed write destroyed state")
	}
	b.failBefore = false
	b.failAfter = true
	if err = s.Update(ctx, 2, func(*State) error { return nil }); !errors.Is(err, ErrUncertain) {
		t.Fatal("lost ACK reported success")
	}
	b.failAfter = false
	current, err := s.Load(ctx)
	if err != nil || current.Revision != 3 {
		t.Fatal("committed state not recovered", err)
	}
	if !bytes.Equal(current.Profiles[profileID].Keys.WireGuardPrivate, original.Profiles[profileID].Keys.WireGuardPrivate) {
		t.Fatal("lost ACK generated another key")
	}
}

func TestCorruptionUnknownVersionAndNoReset(t *testing.T) {
	s, b := fixture(t)
	ctx := context.Background()
	if err := s.Initialize(ctx); !errors.Is(err, ErrStale) {
		t.Fatal("existing state reset")
	}
	for _, data := range [][]byte{{}, []byte("invalid"), bytes.Repeat([]byte{1}, 2<<20+1)} {
		b.data = data
		original := append([]byte(nil), data...)
		if _, err := s.Load(ctx); !errors.Is(err, ErrCorrupt) {
			t.Fatal("corrupt state accepted")
		}
		if err := s.Initialize(ctx); errors.Is(err, ErrMissing) || err == nil {
			t.Fatal("corruption reset")
		}
		if !bytes.Equal(original, b.data) {
			t.Fatal("invalid data rewritten")
		}
	}
	data, err := json.Marshal(State{Version: 99, InstallationID: installation, Revision: 1, Profiles: map[string]Profile{}})
	if err != nil {
		t.Fatal(err)
	}
	b.data, err = s.protector.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatal("unknown version silently migrated")
	}
	b.data = nil
	if _, err = s.Load(ctx); !errors.Is(err, ErrMissing) {
		t.Fatal("missing state auto-initialized")
	}
}

func TestConcurrentInitialClaimCAS(t *testing.T) {
	s, _ := fixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Update(context.Background(), 1, addAttempt) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrStale) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("initial state claim not atomic")
	}
}
