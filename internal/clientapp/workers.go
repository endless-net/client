// Package clientapp owns the public Client's application composition. It does
// not install a service or open the installed Client's protected state.
package clientapp

import (
	"context"
	"errors"
	"sync"

	"go.uber.org/fx"
)

var ErrWorkers = errors.New("client mandatory worker failed")
var ErrLifecycle = errors.New("invalid client worker lifecycle")

// Workers binds the enrollment executor and local IPC serving loop to one Fx
// lifetime. Resource acquisition and IPC self-bootstrap belong to earlier
// application hooks; Healthy alone is not application readiness.
type Workers struct {
	mu                      sync.Mutex
	ipc, enrollment         func(context.Context) error
	shutdown                fx.Shutdowner
	signals                 *Signals
	cancel                  context.CancelFunc
	done                    chan struct{}
	used, running, stopping bool
	failed                  bool
}

// NewWorkers starts nothing and copies no private application implementation.
// Both loops are mandatory and must return on context cancellation. Fx must be
// configured with explicit startup/stop budgets by the complete application.
func NewWorkers(lifecycle fx.Lifecycle, shutdown fx.Shutdowner, signals *Signals, ipc, enrollment func(context.Context) error) (*Workers, error) {
	if lifecycle == nil || shutdown == nil || !signals.valid() || ipc == nil || enrollment == nil {
		return nil, ErrLifecycle
	}
	w := &Workers{ipc: ipc, enrollment: enrollment, shutdown: shutdown, signals: signals}
	lifecycle.Append(signals.hook("workers", w.start, w.stop))
	return w, nil
}

func (w *Workers) start(ctx context.Context) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil {
		return ErrLifecycle
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.used || w.stopping {
		return ErrLifecycle
	}
	w.used, w.running = true, true
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel, w.done = cancel, make(chan struct{})
	var joined sync.WaitGroup
	joined.Add(2)
	for _, worker := range []struct {
		component string
		run       func(context.Context) error
	}{{"ipc", w.ipc}, {"enrollment", w.enrollment}} {
		go func() {
			defer joined.Done()
			_ = w.signals.observe(runCtx, worker.component, "run", func(ctx context.Context) error {
				_ = worker.run(ctx)
				w.mu.Lock()
				unexpected := !w.stopping && !w.failed
				if unexpected {
					w.failed = true
					w.cancel()
				}
				w.mu.Unlock()
				if unexpected {
					// Health/cancellation/shutdown must precede telemetry delivery.
					// Only one worker may call Fx's concurrency-unsafe ExitCode.
					_ = w.shutdown.Shutdown(fx.ExitCode(1))
					return ErrWorkers
				}
				return ctx.Err()
			})
		}()
	}
	go func() { joined.Wait(); close(w.done) }()
	return nil
}

func (w *Workers) stop(ctx context.Context) error {
	w.mu.Lock()
	w.stopping = true
	if w.cancel == nil {
		w.mu.Unlock()
		return nil
	}
	w.cancel()
	done := w.done
	w.mu.Unlock()
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrLifecycle
	}
	select {
	case <-ctx.Done():
		return ErrLifecycle
	case <-done:
		if ctx.Err() != nil {
			return ErrLifecycle
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.running = false
		if w.failed {
			return ErrWorkers
		}
		return nil
	}
}
