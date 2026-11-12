package clientapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
)

func TestRecoveryPreservesIntentAndSerializesOwner(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8)}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	first := awaitWorkerSession(t, h)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
	before, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w.Recover(workerProfile, 1)
	second := awaitWorkerSession(t, h)
	if first.lifetime.Err() == nil || second.generation != first.generation {
		t.Fatal("overlapping owner or changed durable generation")
	}
	select {
	case call := <-second.connectCalls:
		if !call.restore {
			t.Fatal("recovery must obtain fresh authority")
		}
	case <-time.After(time.Second):
		t.Fatal("missing restore")
	}
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
	after, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != after.Revision || before.Profiles[workerProfile].Attempt.NodeID != after.Profiles[workerProfile].Attempt.NodeID {
		t.Fatal("recovery mutated identity or intent")
	}
	// A delayed event from a different durable generation cannot reconnect.
	w.Recover(workerProfile, 99)
	select {
	case <-h.created:
		t.Fatal("stale recovery admitted")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestRecoveryCoalescesEventsAndExpiresOriginalSuspendInclusiveBudget(t *testing.T) {
	s, _ := workerStore(t)
	accepted := time.Now().UTC()
	if err := workerIntent(t, s, state.IntentConnected, accepted, workerRuntime); err != nil {
		t.Fatal(err)
	}
	clock := &workerMutableClock{sample: connectivity.ClockSample{Wall: accepted, Uptime: time.Duration(accepted.UnixNano()), UpperUptime: time.Duration(accepted.UnixNano()), Epoch: workerRuntime}}
	gate := make(chan struct{})
	created := 0
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		created++
		if created > 1 {
			s.connectGate = gate
		}
	}}
	w := testConnectionWorker(t, s, h)
	w.config.Clock = clock
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
	w.Recover(workerProfile, 1)
	second := awaitWorkerSession(t, h)
	select {
	case <-second.connectCalls:
	case <-time.After(time.Second):
		t.Fatal("missing recovery")
	}
	for range 20 {
		w.Recover(workerProfile, 1)
	}
	clock.advance(31*time.Second, 31*time.Second)
	v := awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed })
	if !errors.Is(v.Failure, context.DeadlineExceeded) {
		t.Fatal("budget exhaustion not terminal", v.Failure)
	}
	select {
	case <-h.created:
		t.Fatal("event storm reset budget")
	case <-time.After(25 * time.Millisecond):
	}
}

type recoveryLostSession struct {
	*workerSession
	loss <-chan error
}

func (s *recoveryLostSession) Serve(ctx context.Context) error {
	s.serveCalls <- struct{}{}
	select {
	case err := <-s.loss:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRecoveryAutomaticallyHandlesPathLossButNotSafetyFailures(t *testing.T) {
	for _, failure := range []error{connectivity.FailurePath, connectivity.FailureControl, connectivity.FailureAccess, connectivity.ErrTrust, connectivity.ErrCredentialExpired, connectivity.ErrClock, connectivity.FailureConflict} {
		t.Run(failure.Error(), func(t *testing.T) {
			s, _ := workerStore(t)
			if err := workerIntent(t, s, state.IntentConnected, time.Now(), workerRuntime); err != nil {
				t.Fatal(err)
			}
			h := &workerHarness{created: make(chan *workerSession, 8)}
			loss := make(chan error, 1)
			factory := func(ctx context.Context, p string, g uint64) (ConnectionSession, error) {
				base, err := h.factory(ctx, p, g)
				return &recoveryLostSession{base.(*workerSession), loss}, err
			}
			w := testConnectionWorker(t, s, h)
			w.factory = factory
			startConnectionWorker(t, w)
			awaitWorkerSession(t, h)
			awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
			loss <- failure
			if recoveryFailure(failure) {
				awaitWorkerSession(t, h)
				awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
			} else {
				awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed && !v.CleanupPending })
				select {
				case <-h.created:
					t.Fatal("safety failure automatically retried")
				case <-time.After(20 * time.Millisecond):
				}
			}
		})
	}
}

func TestRecoveryDisconnectPreemptsBlockedRestore(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentConnected, time.Now(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	count := 0
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		count++
		if count == 2 {
			s.connectGate = gate
		}
	}}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
	w.Recover(workerProfile, 1)
	recovering := awaitWorkerSession(t, h)
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now(), workerRuntime); err != nil {
		t.Fatal(err)
	}
	if err := w.Reconcile(workerCallContext(t), workerProfile, 2, true); err != nil {
		t.Fatal(err)
	}
	if recovering.lifetime.Err() == nil {
		t.Fatal("old recovery owner still authorized")
	}
	w.Recover(workerProfile, 1)
	close(gate)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionDisconnected && v.Generation == 2 })
}

func TestRecoveryNeverOverridesDisconnectedIntent(t *testing.T) {
	s, _ := workerStore(t)
	if err := workerIntent(t, s, state.IntentDisconnected, time.Now(), workerOldRuntime); err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 8)}
	w := testConnectionWorker(t, s, h)
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionDisconnected })
	w.Recover(workerProfile, 1)
	select {
	case <-h.created:
		t.Fatal("disconnected intent recovered")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestReconcileWaitsForTransientCleanupAndOriginalAttemptRetry(t *testing.T) {
	for _, failure := range []error{connectivity.FailurePath, connectivity.FailureControl} {
		t.Run(failure.Error(), func(t *testing.T) {
			s, _ := workerStore(t)
			if err := workerIntent(t, s, state.IntentConnected, time.Now(), workerRuntime); err != nil {
				t.Fatal(err)
			}
			before, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			cleanup, connect := make(chan struct{}), make(chan struct{})
			count := 0
			h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
				count++
				if count == 1 {
					s.connectErr, s.cleanupGate = failure, cleanup
				} else {
					s.connectGate = connect
				}
			}}
			w := testConnectionWorker(t, s, h)
			startConnectionWorker(t, w)
			first := awaitWorkerSession(t, h)
			original := <-first.connectCalls
			awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed && v.CleanupPending })
			done := make(chan error, 1)
			wait := workerCallContext(t)
			go func() { done <- w.Reconcile(wait, workerProfile, 1, true) }()
			select {
			case err := <-done:
				t.Fatal("intermediate failure escaped while cleanup can still allow retry", err)
			case <-time.After(20 * time.Millisecond):
			}
			close(cleanup)
			second := awaitWorkerSession(t, h)
			retry := <-second.connectCalls
			if !retry.restore || !retry.deadline.Equal(original.deadline) || first.lifetime.Err() == nil || second.generation != first.generation {
				t.Fatal("retry reset deadline/generation or overlapped native owners")
			}
			select {
			case err := <-done:
				t.Fatal("cleanup completion escaped as terminal failure before retry", err)
			default:
			}
			close(connect)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("successful retry did not complete original wait")
			}
			after, err := s.Load(context.Background())
			if err != nil || after.Revision != before.Revision || *after.Profiles[workerProfile].Connection.Request != *before.Profiles[workerProfile].Connection.Request || after.Profiles[workerProfile].Connection.Deadline != before.Profiles[workerProfile].Connection.Deadline {
				t.Fatal("retry changed durable request or deadline", err)
			}
		})
	}
}

func TestReconcileDoesNotWaitThroughSafetyOrCleanupFailure(t *testing.T) {
	for _, failure := range []error{connectivity.FailureTrust, connectivity.FailureAccess, connectivity.FailureControl} {
		t.Run(failure.Error(), func(t *testing.T) {
			s, _ := workerStore(t)
			if err := workerIntent(t, s, state.IntentConnected, time.Now(), workerRuntime); err != nil {
				t.Fatal(err)
			}
			cleanup := make(chan struct{})
			defer close(cleanup)
			h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
				s.connectErr, s.cleanupGate = failure, cleanup
				if failure == connectivity.FailureControl {
					s.cleanupFailures.Store(2)
				}
			}}
			w := testConnectionWorker(t, s, h)
			startConnectionWorker(t, w)
			awaitWorkerSession(t, h)
			want := failure
			if failure == connectivity.FailureControl {
				want = ErrConnectionOperation
			}
			awaitWorkerView(t, w, func(v ConnectionObservation) bool {
				return v.Phase == ConnectionFailed && errors.Is(v.Failure, want) && (failure != connectivity.FailureControl || v.CleanupFailure != nil)
			})
			if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); !errors.Is(err, want) {
				t.Fatal("terminal failure hidden", err)
			}
			if len(h.created) != 0 {
				t.Fatal("terminal failure retried")
			}
		})
	}
}

func TestReconcileTransientWaitHonorsCancelAndOriginalBudget(t *testing.T) {
	s, _ := workerStore(t)
	accepted := time.Now().UTC()
	if err := workerIntent(t, s, state.IntentConnected, accepted, workerRuntime); err != nil {
		t.Fatal(err)
	}
	cleanup := make(chan struct{})
	h := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) {
		s.connectErr, s.cleanupGate = connectivity.FailureControl, cleanup
	}}
	w := testConnectionWorker(t, s, h)
	clock := &workerMutableClock{sample: connectivity.ClockSample{Wall: accepted, Uptime: time.Duration(accepted.UnixNano()), UpperUptime: time.Duration(accepted.UnixNano()), Epoch: workerRuntime}}
	w.config.Clock = clock
	startConnectionWorker(t, w)
	awaitWorkerSession(t, h)
	awaitWorkerView(t, w, func(v ConnectionObservation) bool { return v.Phase == ConnectionFailed && v.CleanupPending })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Reconcile(ctx, workerProfile, 1, true); !errors.Is(err, context.Canceled) {
		t.Fatal("transient wait ignored caller cancellation", err)
	}
	clock.advance(31*time.Second, 31*time.Second)
	close(cleanup)
	if err := w.Reconcile(workerCallContext(t), workerProfile, 1, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup extended original operation budget", err)
	}
	if len(h.created) != 0 {
		t.Fatal("expired original attempt created another owner")
	}
}
