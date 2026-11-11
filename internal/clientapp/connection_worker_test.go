package clientapp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

const workerInstallation = "c95eea9d-987a-4204-ae10-3085f31fd2fc"
const workerProfile = "d978bf02-a1ac-40d5-a65d-49c0dd931d9d"
const workerRuntime = "0ddc5a0a-090a-4939-a5f5-de63278d2830"
const workerOldRuntime = "d2159714-8243-430b-89e5-e0e1dec57201"

type workerMemory struct {
	mu   sync.Mutex
	data []byte
	lose atomic.Bool
}

func (b *workerMemory) Exclusive(ctx context.Context, fn func(state.Slot) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(b)
}
func (b *workerMemory) Read(limit int64) ([]byte, error) {
	if b.data == nil {
		return nil, state.ErrMissing
	}
	if int64(len(b.data)) > limit {
		return nil, state.ErrCorrupt
	}
	return bytes.Clone(b.data), nil
}
func (b *workerMemory) Replace(data []byte) error {
	b.data = bytes.Clone(data)
	if b.lose.Swap(false) {
		return state.ErrUncertain
	}
	return nil
}

type workerCodec struct{ cipher.AEAD }

func (c workerCodec) Seal(data []byte) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(nonce, nonce, data, nil), nil
}
func (c workerCodec) Open(data []byte) ([]byte, error) {
	if len(data) < c.NonceSize() {
		return nil, state.ErrCorrupt
	}
	return c.AEAD.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], nil)
}

func workerStore(t *testing.T) (*state.Store, *workerMemory) {
	return workerStorePrepared(t, nil)
}

func workerStorePrepared(t *testing.T, prepare func(*state.Profile)) (*state.Store, *workerMemory) {
	t.Helper()
	b := &workerMemory{}
	block, err := aes.NewCipher(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.New(b, workerCodec{aead}, workerInstallation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
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
	now := time.Now().UTC().Add(-time.Hour)
	a, err := enrollment.New(enrollment.Binding{AttemptID: workerRuntime, RequestID: workerOldRuntime, ProfileID: workerProfile, Owner: "fixture-owner", ControlOrigin: "https://control.example", DeviceKeyHash: fingerprint}, now)
	if err != nil {
		t.Fatal(err)
	}
	a.Phase = enrollment.Registered
	a.AccountID = "account"
	a.NetworkID = "network"
	a.NodeID = "node"
	a.CredentialEnds = time.Now().Add(time.Hour)
	if err := s.Update(context.Background(), 1, func(next *state.State) error {
		next.OwnerSID = "fixture-owner"
		next.ActiveProfileID = workerProfile
		profile := state.Profile{ID: workerProfile, ControlOrigin: a.ControlOrigin, Keys: keys, Attempt: &a, Secrets: state.RemoteSecrets{RequestProof: "fixture-request-proof-0123456789012345", RegistrationID: workerOldRuntime, RegistrationRequest: "fixture request", NodeCredential: "enc_fixture"}}
		if prepare != nil {
			prepare(&profile)
		}
		next.Profiles = map[string]state.Profile{workerProfile: profile}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, b
}

func workerIntent(t *testing.T, s *state.Store, intent state.DesiredIntent, at time.Time, runtime string) error {
	t.Helper()
	value, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s.Update(context.Background(), value.Revision, func(next *state.State) error {
		p := next.Profiles[workerProfile]
		if p.Connection == nil {
			p.Connection = &state.ConnectionState{}
		}
		_, err := p.Connection.BeginRequestedAttempt(intent, at, state.ConnectionRequest{InstanceID: runtime, Revision: value.Revision, ClockEpoch: runtime, Uptime: time.Duration(at.UnixNano()), UpperUptime: time.Duration(at.UnixNano())})
		next.Profiles[workerProfile] = p
		return err
	})
}

type workerConnectCall struct {
	restore  bool
	deadline time.Time
}
type workerSession struct {
	lifetime                 context.Context
	generation               uint64
	connectGate, cleanupGate <-chan struct{}
	connectErr               error
	ignoreCancel             bool
	connectCalls             chan workerConnectCall
	cleanupCalls             chan struct{}
	serveCalls               chan struct{}
	cleanupFailures          atomic.Int64
	local                    connectivity.LocalObservation
}

func (s *workerSession) ConnectionLocal() connectivity.LocalObservation {
	if s.lifetime.Err() != nil {
		return connectivity.LocalObservation{}
	}
	return s.local
}

func (s *workerSession) Connect(ctx context.Context, restore bool) error {
	deadline, _ := ctx.Deadline()
	s.connectCalls <- workerConnectCall{restore, deadline}
	if s.ignoreCancel {
		<-s.connectGate
		return s.connectErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.connectGate:
		return s.connectErr
	}
}
func (s *workerSession) Serve(ctx context.Context) error {
	s.serveCalls <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}
func (s *workerSession) Disconnect(ctx context.Context) error {
	s.cleanupCalls <- struct{}{}
	if s.cleanupFailures.Add(-1) >= 0 {
		return errors.New("uncertain native close")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.cleanupGate:
		return nil
	}
}

func workerReadyGate() <-chan struct{} { c := make(chan struct{}); close(c); return c }

type workerHarness struct {
	created chan *workerSession
	setup   func(*workerSession)
}

func (h *workerHarness) factory(ctx context.Context, _ string, generation uint64) (ConnectionSession, error) {
	s := &workerSession{lifetime: ctx, generation: generation, connectGate: workerReadyGate(), cleanupGate: workerReadyGate(), connectCalls: make(chan workerConnectCall, 2), cleanupCalls: make(chan struct{}, 8), serveCalls: make(chan struct{}, 1)}
	if h.setup != nil {
		h.setup(s)
	}
	h.created <- s
	return s, nil
}
func testConnectionWorker(t *testing.T, s *state.Store, h *workerHarness) *ConnectionWorker {
	t.Helper()
	w, err := NewConnectionWorker(s, h.factory, ConnectionWorkerConfig{Clock: workerFixtureClock{}, RuntimeID: workerRuntime, ReadTimeout: 100 * time.Millisecond, PollInterval: 5 * time.Millisecond, CleanupTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestConnectionWorkerCancelledBeforeStartIsNotFailedCleanup(t *testing.T) {
	s, backend := workerStore(t)
	w := testConnectionWorker(t, s, &workerHarness{})
	before := bytes.Clone(backend.data)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Run(ctx); err != context.Canceled || w.used || w.running || !bytes.Equal(before, backend.data) {
		t.Fatal("pre-start cancellation acquired a worker or became a cleanup failure", err)
	}
}

type workerFixtureClock struct{}

func (workerFixtureClock) Sample() (connectivity.ClockSample, error) {
	now := time.Now().UTC()
	uptime := time.Duration(now.UnixNano())
	return connectivity.ClockSample{Wall: now, Uptime: uptime, UpperUptime: uptime, Epoch: workerRuntime}, nil
}
func startConnectionWorker(t *testing.T, w *ConnectionWorker) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("worker did not terminate")
		}
	})
	return cancel, done
}
func awaitWorkerView(t *testing.T, w *ConnectionWorker, match func(ConnectionObservation) bool) ConnectionObservation {
	t.Helper()
	bound := time.NewTimer(2 * time.Second)
	defer bound.Stop()
	for {
		w.mu.Lock()
		v, changed := w.view, w.changed
		w.mu.Unlock()
		if match(v) {
			return v
		}
		select {
		case <-changed:
		case <-bound.C:
			t.Fatal("worker observation timeout", v)
		}
	}
}
func awaitWorkerSession(t *testing.T, h *workerHarness) *workerSession {
	t.Helper()
	select {
	case s := <-h.created:
		return s
	case <-time.After(time.Second):
		t.Fatal("owner not created")
		return nil
	}
}
func workerCallContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestConnectionWorkerDuplicateWaitDoesNotOwnSessionLifetime(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) { s.connectGate = gate }}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	owner := awaitWorkerSession(t, h)
	before, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := w.Reconcile(ctx, workerProfile, 1, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if owner.lifetime.Err() != nil {
		t.Fatal("wait timeout cancelled accepted session")
	}
	for range 20 {
		if err := w.Reconcile(workerCallContext(t), workerProfile, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	close(gate)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
	after, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || *after.Profiles[workerProfile].Connection.Request != *before.Profiles[workerProfile].Connection.Request || after.Profiles[workerProfile].Connection.Deadline != before.Profiles[workerProfile].Connection.Deadline || len(h.created) != 0 {
		t.Fatal("duplicate changed durable attempt or created another owner")
	}
}

func TestConnectionWorkerSupersessionWaitsForOldCleanupAndCoalescesLatestIntent(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	cleanup := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		if s.generation == 1 {
			s.cleanupGate = cleanup
		}
	}}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	old := awaitWorkerSession(t, h)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, false); err != nil {
		t.Fatal(err)
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 2 })
	select {
	case <-old.cleanupCalls:
	case <-time.After(time.Second):
		t.Fatal("old cleanup not invoked")
	}
	if old.lifetime.Err() == nil || len(h.created) != 0 {
		t.Fatal("new owner bypassed live old owner")
	}
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 3, false); err != nil {
		t.Fatal(err)
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 3 })
	close(cleanup)
	latest := awaitWorkerSession(t, h)
	if latest.generation != 3 {
		t.Fatal("obsolete queued intent executed")
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 3, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, true); !errors.Is(err, state.ErrStale) {
		t.Fatal("old waiter claimed success", err)
	}
}

func TestConnectionWorkerPollFindsCommittedIntentAfterLostACKWithoutWake(t *testing.T) {
	s, b := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8)}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b.lose.Store(true)
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now().UTC(), workerRuntime); !errors.Is(err, state.ErrUncertain) {
		t.Fatal(err)
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 2 && v.Phase == ConnectionDisconnected })
	after, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 || !bytes.Equal(after.Profiles[workerProfile].Keys.WireGuardPrivate, before.Profiles[workerProfile].Keys.WireGuardPrivate) {
		t.Fatal("poll reset state/keys")
	}
}

func TestConnectionWorkerRestartUsesFreshBoundWithoutRewritingOldIntent(t *testing.T) {
	for _, intent := range []state.DesiredIntent{state.IntentConnected, state.IntentDisconnected} {
		t.Run(string(intent), func(t *testing.T) {
			s, _ := workerStore(t)
			if err := workerIntent(t, s, intent, time.Now().UTC().Add(-2*time.Minute), workerOldRuntime); err != nil {
				t.Fatal(err)
			}
			before, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			h := &workerHarness{created: make(chan *workerSession, 8)}
			w := testConnectionWorker(t, s, h)
			startConnectionWorker(t, w)
			owner := awaitWorkerSession(t, h)
			if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
				t.Fatal(err)
			}
			if intent == state.IntentConnected {
				call := <-owner.connectCalls
				if !call.restore || time.Until(call.deadline) <= 0 || time.Until(call.deadline) > 30*time.Second {
					t.Fatal("restore reused old budget")
				}
			} else if len(owner.connectCalls) != 0 {
				t.Fatal("disconnected restart requested authority")
			}
			after, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision || after.Profiles[workerProfile].Connection.Deadline != before.Profiles[workerProfile].Connection.Deadline || after.Profiles[workerProfile].Connection.AcceptedAt != before.Profiles[workerProfile].Connection.AcceptedAt {
				t.Fatal("restore rewrote accepted intent")
			}
		})
	}
}

func TestConnectionWorkerTerminalFailureNeedsNewRequestedGeneration(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		if s.generation == 1 {
			s.connectErr = errors.New("secret backend diagnostics")
		}
	}}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	v := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed && !v.CleanupPending })
	if v.Failure != ErrConnectionOperation || !w.Retryable(workerProfile, 1) || w.Retryable(workerProfile, 2) {
		t.Fatal("invalid failure/retry observation")
	}
	for range 20 {
		if err := w.Reconcile(workerCallContext(t), workerProfile, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); !errors.Is(err, ErrConnectionOperation) {
		t.Fatal(err)
	}
	if len(h.created) != 0 {
		t.Fatal("terminal generation silently retried")
	}
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, true); err != nil {
		t.Fatal(err)
	}
	if owner := awaitWorkerSession(t, h); owner.generation != 2 {
		t.Fatal("new retry not fenced")
	}
}

func TestConnectionWorkerDeadlineRejectsLateSuccessWhileRetainingOwnerBarrier(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC().Add(-30*time.Second+70*time.Millisecond), workerRuntime); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		if s.generation == 1 {
			s.ignoreCancel = true
			s.connectGate = gate
		}
	}}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	old := awaitWorkerSession(t, h)
	v := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed })
	if v.Failure != context.DeadlineExceeded || old.lifetime.Err() == nil {
		t.Fatal("deadline not terminal")
	}
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, false); err != nil {
		t.Fatal(err)
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 2 })
	if len(h.created) != 0 {
		t.Fatal("new owner started while late apply still owned resources")
	}
	close(gate)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, true); err != nil {
		t.Fatal(err)
	}
	if owner := awaitWorkerSession(t, h); owner.generation != 2 {
		t.Fatal("late success superseded current intent")
	}
}

func TestConnectionWorkerPendingDisconnectHasDeadlineDuringUnresolvedOldApply(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC().Add(-29*time.Second), workerRuntime); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) { s.ignoreCancel = true; s.connectGate = gate }}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	old := awaitWorkerSession(t, h)
	<-old.connectCalls
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now().UTC().Add(-10*time.Second+100*time.Millisecond), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, false); err != nil {
		t.Fatal(err)
	}
	v := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 2 && v.Phase == ConnectionFailed })
	if v.Failure != context.DeadlineExceeded || !v.CleanupPending || old.lifetime.Err() == nil || len(h.created) != 0 {
		t.Fatal("pending disconnect exceeded budget or bypassed old owner")
	}
	close(gate)
	select {
	case <-old.cleanupCalls:
	case <-time.After(time.Second):
		t.Fatal("late apply did not clean up")
	}
	if v := w.Observe(); v.Generation != 2 || v.Phase != ConnectionFailed || len(h.created) != 0 {
		t.Fatal("late old success changed timed-out intent")
	}
}

func TestConnectionWorkerShutdownIsBoundedAndRetainsUnconfirmedCleanup(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) { s.ignoreCancel = true; s.connectGate = gate }}
	w := testConnectionWorker(t, s, h)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	old := awaitWorkerSession(t, h)
	<-old.connectCalls
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrLifecycle) {
			t.Fatal("unconfirmed cleanup claimed orderly shutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown waited without a bound")
	}
	v := w.Observe()
	if old.lifetime.Err() == nil || !v.CleanupPending || v.Phase != ConnectionFailed || w.Retryable(workerProfile, 1) {
		t.Fatal("stopped worker retained authority or accepted retries")
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, false); !errors.Is(err, ErrConnectionWorker) {
		t.Fatal("stopped worker accepted command", err)
	}
	close(gate)
	select {
	case <-old.cleanupCalls:
	case <-time.After(time.Second):
		t.Fatal("late owner leaked after shutdown")
	}
	if err := w.Run(context.Background()); !errors.Is(err, ErrConnectionWorker) {
		t.Fatal("worker lifetime reused", err)
	}
}

func TestConnectionWorkerFailedCleanupRetainsOriginalOwnerForExplicitRetry(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		if s.generation == 1 {
			s.cleanupFailures.Store(2)
		}
	}}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	old := awaitWorkerSession(t, h)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, true); !errors.Is(err, ErrConnectionOperation) {
		t.Fatal("uncertain cleanup claimed success", err)
	}
	view := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Generation == 2 && v.Phase == ConnectionFailed })
	if !view.CleanupPending || len(h.created) != 0 || len(old.cleanupCalls) != 2 {
		t.Fatal("failed owner dropped or replaced before successful close")
	}
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now().UTC(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 3, true); err != nil {
		t.Fatal(err)
	}
	if next := awaitWorkerSession(t, h); next.generation != 3 || len(old.cleanupCalls) != 3 {
		t.Fatal("retry did not close original owner before replacement")
	}
}

type workerMutableClock struct {
	mu     sync.Mutex
	sample connectivity.ClockSample
}

func (c *workerMutableClock) Sample() (connectivity.ClockSample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sample, nil
}
func (c *workerMutableClock) advance(wall, uptime time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sample.Wall = c.sample.Wall.Add(wall)
	c.sample.Uptime += uptime
	c.sample.UpperUptime += uptime
}

func TestConnectionWorkerOriginalCounterCoversDelayBeforeFirstWakeAndSuspend(t *testing.T) {
	for _, beforeStart := range []bool{true, false} {
		s, _ := workerStore(t)
		accepted := time.Now().UTC()
		if err := workerIntent(t, s, state.IntentConnected, accepted, workerRuntime); err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) { s.connectGate = gate }}
		clock := &workerMutableClock{sample: connectivity.ClockSample{Wall: accepted, Uptime: time.Duration(accepted.UnixNano()), UpperUptime: time.Duration(accepted.UnixNano()), Epoch: workerRuntime}}
		if beforeStart {
			clock.advance(time.Second, 31*time.Second)
		}
		w := testConnectionWorker(t, s, h)
		w.config.Clock = clock
		startConnectionWorker(t, w)
		var owner *workerSession
		if !beforeStart {
			owner = awaitWorkerSession(t, h)
			<-owner.connectCalls
			clock.advance(time.Second, 31*time.Second)
		}
		v := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed })
		if v.Failure != context.DeadlineExceeded || len(h.created) != 0 {
			t.Fatal("original counter deadline reset on first observation/resume")
		}
		if owner != nil && owner.lifetime.Err() == nil {
			t.Fatal("suspend left old lifetime active")
		}
		close(gate)
	}
}

func TestConnectionWorkerConfirmedConnectDoesNotExpireAtOperationDeadline(t *testing.T) {
	s, _ := workerStore(t)
	accepted := time.Now().UTC()
	if err := workerIntent(t, s, state.IntentConnected, accepted, workerRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8)}
	clock := &workerMutableClock{sample: connectivity.ClockSample{Wall: accepted, Uptime: time.Duration(accepted.UnixNano()), UpperUptime: time.Duration(accepted.UnixNano()), Epoch: workerRuntime}}
	w := testConnectionWorker(t, s, h)
	w.config.Clock = clock
	startConnectionWorker(t, w)
	owner := awaitWorkerSession(t, h)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal(err)
	}
	clock.advance(31*time.Second, 31*time.Second)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); err != nil {
		t.Fatal("completed command became a session duration limit", err)
	}
	if owner.lifetime.Err() != nil || w.Observe().Phase != ConnectionConnected {
		t.Fatal("operation budget replaced continuing authority lifecycle")
	}
}
