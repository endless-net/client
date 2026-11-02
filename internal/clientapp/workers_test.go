package clientapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

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
