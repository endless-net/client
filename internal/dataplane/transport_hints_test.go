package dataplane

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
)

// An authenticated test codec and memory backend exercise the real protected
// State Store/CAS. They implement no production DPAPI or filesystem fallback.
type hintTestCodec struct{ cipher.AEAD }

func (c hintTestCodec) Seal(raw []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, raw, nil), nil
}
func (c hintTestCodec) Open(raw []byte) ([]byte, error) {
	if len(raw) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, raw[:c.NonceSize()], raw[c.NonceSize():], nil)
}

type hintTestBackend struct {
	mu        sync.Mutex
	data      []byte
	uncertain bool
}

func (b *hintTestBackend) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fn(b)
}
func (b *hintTestBackend) Read(limit int64) ([]byte, error) {
	if int64(len(b.data)) > limit {
		return nil, state.ErrCorrupt
	}
	return bytes.Clone(b.data), nil
}
func (b *hintTestBackend) Replace(raw []byte) error {
	b.data = bytes.Clone(raw)
	if b.uncertain {
		b.uncertain = false
		return state.ErrUncertain
	}
	return nil
}

func hintStoreFixture(t *testing.T, f *candidateFixture) (*state.Store, *hintTestBackend) {
	t.Helper()
	p := f.profile
	p.Secrets.RequestProof = strings.Repeat("fixture-request-proof", 2)
	p.Secrets.RegistrationID, p.Secrets.RegistrationRequest = p.Attempt.RequestID, "fixture original request"
	const installation = "d3f97458-1548-48f7-b053-48c03342bf43"
	v := state.State{Version: state.CurrentVersion, InstallationID: installation, Revision: 1, OwnerSID: p.Attempt.Owner, ActiveProfileID: p.ID, Profiles: map[string]state.Profile{p.ID: p}}
	if err := v.Validate(installation); err != nil {
		t.Fatal("hint fixture rejected", err)
	}
	block, err := aes.NewCipher(bytes.Repeat([]byte{87}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	codec := hintTestCodec{aead}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	sealed, err := codec.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	backend := &hintTestBackend{data: sealed}
	store, err := state.New(backend, codec, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	return store, backend
}

func TestRuntimeHintStorageUsesCurrentContextAndNeverRenewsSuccess(t *testing.T) {
	f := newCandidateFixture(t)
	store, backend := hintStoreFixture(t, f)
	digest := strings.Repeat("1", 64)
	h := &transportHintRuntime{ctx: t.Context(), store: store, clock: f.clock, lease: f.lease, profile: f.profile.ID, generation: 1, source: func(context.Context, []byte) (string, error) {
		if digest == "" {
			return "", ErrNoAuthority
		}
		return digest, nil
	}}
	sample, _ := f.clock.Sample()
	keys, err := hintKeys(f.verified, f.profile.ID, digest, sample.Wall)
	if err != nil || len(keys) != 1 {
		t.Fatal("authorized binding missing", err)
	}
	success := state.TransportHint{Key: keys[0], SucceededAt: sample.Wall, ExpiresAt: sample.Wall.Add(state.TransportHintTTL), RTT: time.Millisecond, StableSpan: 5 * time.Second, SuccessfulProbes: 3}
	backend.uncertain = true
	result, err := h.operate(t.Context(), f.verified, map[state.TransportHintKey]*state.TransportHint{keys[0]: &success}, false)
	if err != nil || len(result.preferred) != 1 {
		t.Fatal("committed ACK loss did not reconcile", err)
	}
	f.clock.advance(time.Second, time.Second)
	result, err = h.operate(t.Context(), f.verified, nil, false)
	if err != nil || len(result.preferred) != 1 {
		t.Fatal("restart-equivalent lookup lost success", err)
	}
	v, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearHintStoreKeys(v)
	if len(v.TransportHints) != 1 || !v.TransportHints[0].SucceededAt.Equal(success.SucceededAt) || !v.TransportHints[0].ExpiresAt.Equal(success.ExpiresAt) {
		t.Fatal("lookup renewed cached success")
	}
	result, err = h.operate(t.Context(), f.verified, map[state.TransportHintKey]*state.TransportHint{keys[0]: nil}, false)
	if err != nil || len(result.preferred) != 0 {
		t.Fatal("failed candidate kept its priority", err)
	}
	if _, err := h.operate(t.Context(), f.verified, map[state.TransportHintKey]*state.TransportHint{keys[0]: &success}, false); err != nil {
		t.Fatal(err)
	}
	digest = strings.Repeat("2", 64)
	result, err = h.operate(t.Context(), f.verified, nil, false)
	if err != nil || len(result.preferred) != 0 {
		t.Fatal("network change inherited old priority", err)
	}
	digest = ""
	result, err = h.operate(t.Context(), f.verified, nil, false)
	if err != nil || result.context != "" || len(result.preferred) != 0 {
		t.Fatal("unknown context inherited priority", err)
	}
}

func TestRuntimeHintRejectsOldIntentAndUnauthorizedMap(t *testing.T) {
	f := newCandidateFixture(t)
	store, _ := hintStoreFixture(t, f)
	h := &transportHintRuntime{ctx: t.Context(), store: store, clock: f.clock, lease: f.lease, profile: f.profile.ID, generation: 1, source: func(context.Context, []byte) (string, error) { return strings.Repeat("1", 64), nil }}
	v, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearHintStoreKeys(v)
	if err := store.Update(t.Context(), v.Revision, func(next *state.State) error {
		p := next.Profiles[f.profile.ID]
		_, err := p.Connection.AcceptIntent(state.IntentDisconnected, p.Connection.AcceptedAt.Add(time.Second))
		next.Profiles[p.ID] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.operate(t.Context(), f.verified, nil, false); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("old intent performed hint mutation", err)
	}
	f.lease.Revoke()
	if _, err := h.operate(t.Context(), f.verified, nil, false); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("revoked lease performed hint lookup", err)
	}
}

func TestHintWorkerCoalescesRequestsWithoutReturningAnOlderLookup(t *testing.T) {
	f := newCandidateFixture(t)
	store, _ := hintStoreFixture(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var inFlight, peak atomic.Int32
	h := newTransportHintRuntime(ctx, store, f.clock, f.lease, f.profile.ID, 1, f.profile.Keys.WireGuardPrivate, func(ctx context.Context, _ []byte) (string, error) {
		count := inFlight.Add(1)
		defer inFlight.Add(-1)
		for previous := peak.Load(); count > previous && !peak.CompareAndSwap(previous, count); previous = peak.Load() {
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return strings.Repeat("1", 64), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}, func() {})
	first := h.request(f.verified)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	second := h.request(f.verified)
	if second <= first {
		t.Fatal("new request reused old lookup generation")
	}
	if _, ready := h.lookup(second, f.verified); ready {
		t.Fatal("blocked older lookup satisfied new search")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		if result, ready := h.lookup(second, f.verified); ready {
			if result.err != nil || result.context == "" || peak.Load() != 1 {
				t.Fatal("lookup failed or concurrent I/O escaped bound", result.err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("coalesced new request never completed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("canceled hint worker did not exit")
	}
}

func TestHintWorkerPendingOverflowCannotRetainDroppedInvalidations(t *testing.T) {
	h := &transportHintRuntime{wake: make(chan struct{}, 1), pending: make(map[state.TransportHintKey]*state.TransportHint)}
	for i := 0; i <= state.MaxTransportHints; i++ {
		key := state.TransportHintKey{TransportProfileID: strings.Repeat("x", i+1)}
		h.change(key, nil)
	}
	if len(h.pending) > state.MaxTransportHints || !h.reset {
		t.Fatal("overflow retained old priorities or exceeded pending bound")
	}
}

func TestHintLookupPrunesExpiredOfferWhileMapLeaseRemainsCurrent(t *testing.T) {
	f := newCandidateFixture(t)
	store, _ := hintStoreFixture(t, f)
	digest := strings.Repeat("1", 64)
	h := &transportHintRuntime{ctx: t.Context(), store: store, clock: f.clock, lease: f.lease, profile: f.profile.ID, generation: 1, source: func(context.Context, []byte) (string, error) { return digest, nil }}
	sample, _ := f.clock.Sample()
	keys, err := hintKeys(f.verified, f.profile.ID, digest, sample.Wall)
	if err != nil || len(keys) != 1 {
		t.Fatal("authorized key missing", err)
	}
	success := state.TransportHint{Key: keys[0], SucceededAt: sample.Wall, ExpiresAt: sample.Wall.Add(state.TransportHintTTL), StableSpan: 5 * time.Second, SuccessfulProbes: 3}
	if _, err := h.operate(t.Context(), f.verified, map[state.TransportHintKey]*state.TransportHint{keys[0]: &success}, false); err != nil {
		t.Fatal(err)
	}
	f.planTTL = 2 * time.Second
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	f.clock.advance(2*time.Second, 2*time.Second)
	if !f.lease.Permissions().NewFlows {
		t.Fatal("test expired whole map instead of the Direct offer")
	}
	result, err := h.operate(t.Context(), f.verified, nil, false)
	if err != nil || len(result.preferred) != 0 {
		t.Fatal("expired Direct envelope retained priority", err)
	}
	v, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearHintStoreKeys(v)
	if len(v.TransportHints) != 0 {
		t.Fatal("expired offer remained in protected cache")
	}
}
