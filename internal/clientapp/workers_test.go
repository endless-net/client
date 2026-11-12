package clientapp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"

	"go.uber.org/fx"
)

type hooks struct{ hook fx.Hook }

func (h *hooks) Append(hook fx.Hook) { h.hook = hook }

type shutdown struct{ calls atomic.Int32 }

func (s *shutdown) Shutdown(...fx.ShutdownOption) error { s.calls.Add(1); return nil }

func budget(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitCancelled(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

type shutdownDeadlineSession struct {
	*workerSession
	deadlines chan time.Time
}

func (s *shutdownDeadlineSession) Disconnect(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	s.deadlines <- deadline
	return s.workerSession.Disconnect(ctx)
}

func TestWorkersStopUsesOriginalDeadlineForNativeCleanup(t *testing.T) {
	for _, enough := range []bool{true, false} {
		t.Run(map[bool]string{true: "remaining_stop_budget", false: "expired_stop_budget"}[enough], func(t *testing.T) {
			store, _ := workerStore(t)
			if err := workerIntent(t, store, state.IntentConnected, time.Now(), workerRuntime); err != nil {
				t.Fatal(err)
			}
			gate := make(chan struct{})
			var release sync.Once
			unblock := func() { release.Do(func() { close(gate) }) }
			defer unblock()
			deadlines := make(chan time.Time, 8)
			harness := &workerHarness{created: make(chan *workerSession, 8), setup: func(s *workerSession) { s.cleanupGate = gate }}
			connection := testConnectionWorker(t, store, harness)
			connection.factory = func(ctx context.Context, profile string, generation uint64) (ConnectionSession, error) {
				base, err := harness.factory(ctx, profile, generation)
				return &shutdownDeadlineSession{base.(*workerSession), deadlines}, err
			}
			h, shutdown := &hooks{}, &shutdown{}
			workers, err := NewWorkers(h, shutdown, testSignals(t), waitCancelled, waitCancelled, connection.Run)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.hook.OnStart(budget(t)); err != nil {
				t.Fatal(err)
			}
			awaitWorkerSession(t, harness)
			awaitWorkerView(t, connection, func(v ConnectionObservation) bool { return v.Phase == ConnectionConnected })
			duration := 750 * time.Millisecond
			if !enough {
				duration = 80 * time.Millisecond
			}
			stop, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			original, _ := stop.Deadline()
			done := make(chan error, 1)
			go func() { done <- h.hook.OnStop(stop) }()
			defer func() {
				unblock()
				select {
				case <-workers.done:
				case <-time.After(time.Second):
					t.Error("stop left workers running")
				}
			}()
			select {
			case actual := <-deadlines:
				if !actual.Equal(original) {
					t.Fatal("native cleanup reset or shortened the original stop deadline", actual, original)
				}
			case <-time.After(time.Second):
				t.Fatal("native cleanup did not start")
			}
			if enough {
				select {
				case err := <-done:
					t.Fatal("background cleanup timeout truncated the remaining stop budget", err)
				case <-time.After(connection.config.CleanupTimeout + 30*time.Millisecond):
				}
				unblock()
			}
			select {
			case err := <-done:
				if enough && err != nil || !enough && err == nil {
					t.Fatal("cleanup outcome lost", enough, err)
				}
			case <-time.After(time.Second):
				t.Fatal("native cleanup escaped stop bound")
			}
			if shutdown.calls.Load() != 0 {
				t.Fatal("normal stop became an unexpected application shutdown")
			}
		})
	}
}

func TestWorkersValidateWithoutStarting(t *testing.T) {
	h, s := &hooks{}, &shutdown{}
	if _, err := NewWorkers(h, s, testSignals(t), waitCancelled, waitCancelled, nil); err != ErrLifecycle {
		t.Fatal("missing connection loop accepted", err)
	}
	if _, err := NewWorkers(h, s, nil, waitCancelled, waitCancelled, waitCancelled); err != ErrLifecycle {
		t.Fatal("missing telemetry accepted", err)
	}
	for _, c := range []struct {
		l    fx.Lifecycle
		s    fx.Shutdowner
		i, e func(context.Context) error
	}{{nil, s, waitCancelled, waitCancelled}, {h, nil, waitCancelled, waitCancelled}, {h, s, nil, waitCancelled}, {h, s, waitCancelled, nil}} {
		if _, err := NewWorkers(c.l, c.s, testSignals(t), c.i, c.e, waitCancelled); !errors.Is(err, ErrLifecycle) {
			t.Fatal(err)
		}
	}
	w, err := NewWorkers(h, s, testSignals(t), waitCancelled, waitCancelled, waitCancelled)
	if err != nil || w.Healthy() || s.calls.Load() != 0 {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(context.Background()); !errors.Is(err, ErrLifecycle) {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	if !w.Healthy() {
		t.Fatal("workers not healthy after start")
	}
	if err := h.hook.OnStart(budget(t)); !errors.Is(err, ErrLifecycle) {
		t.Fatal(err)
	}
	if err := h.hook.OnStop(budget(t)); err != nil {
		t.Fatal(err)
	}
	if w.Healthy() || s.calls.Load() != 0 {
		t.Fatal("clean stop reported failure or health")
	}
}

func TestConnectionLoopFailureCancelsMandatorySiblings(t *testing.T) {
	h, s := &hooks{}, &shutdown{}
	release := make(chan struct{})
	w, err := NewWorkers(h, s, testSignals(t), waitCancelled, waitCancelled, func(context.Context) error { <-release; return ErrConnectionWorker })
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-w.done:
	case <-budget(t).Done():
		t.Fatal("connection failure left mandatory siblings running")
	}
	if w.Healthy() || s.calls.Load() != 1 || h.hook.OnStop(budget(t)) != ErrWorkers {
		t.Fatal("connection failure did not fail application lifecycle")
	}
}

func TestConnectionCleanupFailureIsNotHiddenByShutdownCancellation(t *testing.T) {
	h, s := &hooks{}, &shutdown{}
	w, err := NewWorkers(h, s, testSignals(t), waitCancelled, waitCancelled, func(ctx context.Context) error {
		<-ctx.Done()
		return errors.Join(ctx.Err(), ErrLifecycle)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStop(budget(t)); err != ErrWorkers || w.Healthy() || s.calls.Load() != 0 {
		t.Fatal("failed native cleanup reported as clean stop or restarted shutdown", err)
	}
}

func TestSimultaneousWorkerReturnsSignalShutdownOnce(t *testing.T) {
	for range 100 {
		h, s := &hooks{}, &shutdown{}
		release := make(chan struct{})
		run := func(context.Context) error { <-release; return errors.New("synthetic private credential") }
		w, err := NewWorkers(h, s, testSignals(t), run, run, waitCancelled)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.hook.OnStart(budget(t)); err != nil {
			t.Fatal(err)
		}
		close(release)
		select {
		case <-w.done:
		case <-budget(t).Done():
			t.Fatal("workers did not finish")
		}
		if w.Healthy() || s.calls.Load() != 1 {
			t.Fatal("failure did not produce exactly one shutdown")
		}
		if err := h.hook.OnStop(budget(t)); err != ErrWorkers {
			t.Fatal("raw error disclosed or failure lost", err)
		}
	}
}

func TestUnexpectedNilReturnCancelsSibling(t *testing.T) {
	h, s := &hooks{}, &shutdown{}
	w, err := NewWorkers(h, s, testSignals(t), func(context.Context) error { return nil }, waitCancelled, waitCancelled)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.done:
	case <-budget(t).Done():
		t.Fatal("sibling not cancelled")
	}
	if err := h.hook.OnStop(budget(t)); err != ErrWorkers || s.calls.Load() != 1 {
		t.Fatal(err)
	}
}

func TestUnresponsiveWorkerFailsBoundedStop(t *testing.T) {
	h, s := &hooks{}, &shutdown{}
	release := make(chan struct{})
	w, err := NewWorkers(h, s, testSignals(t), func(context.Context) error { <-release; return nil }, waitCancelled, waitCancelled)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(budget(t))
	cancel()
	if err := h.hook.OnStop(ctx); err != ErrLifecycle {
		t.Fatal(err)
	}
	if w.Healthy() {
		t.Fatal("health retained after failed stop")
	}
	close(release)
	if err := h.hook.OnStop(budget(t)); err != nil || s.calls.Load() != 0 {
		t.Fatal(err)
	}
}

func TestActualFxWorkerFailureAndStartupRollback(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{true: "rollback", false: "worker_failure"}[rollback], func(t *testing.T) {
			release := make(chan struct{})
			var workers *Workers
			app := fx.New(fx.NopLogger, fx.StartTimeout(time.Second), fx.StopTimeout(time.Second),
				fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner) error {
					run := waitCancelled
					if !rollback {
						run = func(ctx context.Context) error {
							select {
							case <-release:
								return nil
							case <-ctx.Done():
								return ctx.Err()
							}
						}
					}
					var err error
					workers, err = NewWorkers(lc, shutdown, testSignals(t), run, waitCancelled, waitCancelled)
					if rollback {
						lc.Append(fx.Hook{OnStart: func(context.Context) error { return errors.New("startup fixture") }})
					}
					return err
				}))
			if err := app.Err(); err != nil {
				t.Fatal(err)
			}
			err := app.Start(budget(t))
			if rollback {
				if err == nil || workers.Healthy() {
					t.Fatal("startup failure did not roll back workers")
				}
				select {
				case <-workers.done:
				case <-budget(t).Done():
					t.Fatal("rollback leaked workers")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			shutdown := app.Wait()
			close(release)
			select {
			case signal := <-shutdown:
				if signal.ExitCode != 1 {
					t.Fatal(signal)
				}
			case <-budget(t).Done():
				t.Fatal("no Fx shutdown")
			}
			if err := app.Stop(budget(t)); !errors.Is(err, ErrWorkers) {
				t.Fatal(err)
			}
		})
	}
}
