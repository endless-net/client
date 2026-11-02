package clientapp

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
)

var ErrConnectionWorker = errors.New("connection worker unavailable")
var ErrConnectionOperation = errors.New("connection operation failed")

// ConnectionSession owns one generation, including any uncertain native effects.
// The factory allocates only an owner bound to lifetime; it performs no I/O or
// native mutations. Connect must reconcile previous receipts, obtain fresh
// authority, apply and confirm the selected Direct path within operation's bound.
// Serve owns subsequent authority/endpoint updates and returns on revocation,
// expiry or lifetime cancellation. Disconnect revokes first and confirms own
// cleanup; it also works before Connect and after failed/partial application.
// Repeated Disconnect is proof-checked, idempotent own cleanup. A failed Close
// does not transfer ownership: the same owner is retained for an explicit retry.
// Every method must honor its context. Neither successful local apply nor an
// endpoint ACK alone satisfies Connect's path confirmation requirement.
type ConnectionSession interface {
	Connect(operation context.Context, restore bool) error
	Serve(lifetime context.Context) error
	Disconnect(cleanup context.Context) error
}

type ConnectionFactory func(lifetime context.Context, profile string, generation uint64) (ConnectionSession, error)

type ConnectionWorkerConfig struct {
	Clock                                     connectivity.Clock
	RuntimeID                                 string
	ReadTimeout, PollInterval, CleanupTimeout time.Duration
}

type ConnectionPhase = connectivity.Phase

const (
	ConnectionUnknown       = connectivity.PhaseUnknown
	ConnectionConnecting    = connectivity.PhaseConnecting
	ConnectionConnected     = connectivity.PhaseConnected
	ConnectionDisconnecting = connectivity.PhaseDisconnecting
	ConnectionDisconnected  = connectivity.PhaseDisconnected
	ConnectionFailed        = connectivity.PhaseFailed
)

// ConnectionObservation is volatile and contains no keys, remote grants or raw
// backend diagnostics. It is meaningful only for its exact profile/generation.
type ConnectionObservation = connectivity.Observation

type connectionTarget struct {
	profile    string
	generation uint64
	intent     state.DesiredIntent
	deadline   time.Time
	restore    bool
	receipt    connectivity.ClockSample
	budget     *connectivity.OperationBudget
}

type connectionJob struct {
	target   connectionTarget
	lifetime context.Context
	cancel   context.CancelFunc
	previous ConnectionSession
}

type connectionResult struct {
	job             *connectionJob
	ready, finished bool
	err             error
	retained        ConnectionSession
	observer        connectivity.LocalObservationSource
}

// ConnectionWorker keeps one running owner and one latest durable target. Wakes
// are coalesced because intent is in Store; they are not queued commands. Polling
// detects a committed intent whose ACK/wake was lost, without retrying a terminal
// generation. The state lock is never acquired while the observation lock is held.
type ConnectionWorker struct {
	store         *state.Store
	factory       ConnectionFactory
	config        ConnectionWorkerConfig
	wake          chan struct{}
	mu            sync.Mutex
	used, running bool
	view          ConnectionObservation
	changed       chan struct{}
	retired       ConnectionSession
	observer      connectivity.LocalObservationSource
}

func (*ConnectionWorker) String() string   { return "[private connection worker]" }
func (*ConnectionWorker) GoString() string { return "[private connection worker]" }

func NewConnectionWorker(store *state.Store, factory ConnectionFactory, c ConnectionWorkerConfig) (*ConnectionWorker, error) {
	id, err := uuid.Parse(c.RuntimeID)
	if store == nil || factory == nil || c.Clock == nil || err != nil || id == uuid.Nil || id.String() != c.RuntimeID || c.ReadTimeout < time.Millisecond || c.ReadTimeout > 30*time.Second || c.PollInterval < time.Millisecond || c.PollInterval > 10*time.Second || c.CleanupTimeout < time.Millisecond || c.CleanupTimeout > 10*time.Second {
		return nil, ErrConnectionWorker
	}
	return &ConnectionWorker{store: store, factory: factory, config: c, wake: make(chan struct{}, 1), changed: make(chan struct{})}, nil
}

func (w *ConnectionWorker) Observe() ConnectionObservation {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.view
}

func (w *ConnectionWorker) ConnectionSnapshot() connectivity.Observation {
	w.mu.Lock()
	v, observer := w.view, w.observer
	w.mu.Unlock()
	if observer != nil {
		v.Local = observer.ConnectionLocal()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.view.Profile != v.Profile || w.view.Generation != v.Generation || w.view.Phase != v.Phase {
		return w.view
	}
	return v
}

func (w *ConnectionWorker) Retryable(profile string, generation uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running && w.view.Profile == profile && w.view.Generation == generation && w.view.Phase == ConnectionFailed
}

func (w *ConnectionWorker) announceLocked() {
	close(w.changed)
	w.changed = make(chan struct{})
}

func (w *ConnectionWorker) target(ctx context.Context) (*connectionTarget, error) {
	read, cancel := context.WithTimeout(ctx, w.config.ReadTimeout)
	defer cancel()
	s, err := w.store.Load(read)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, p := range s.Profiles {
			clear(p.Keys.IdentityPrivate)
			clear(p.Keys.WireGuardPrivate)
		}
	}()
	if err := read.Err(); err != nil {
		return nil, err
	}
	p := s.Profiles[s.ActiveProfileID]
	if p.Connection == nil {
		return nil, nil
	}
	c := p.Connection
	restore := c.Request == nil || c.Request.InstanceID != w.config.RuntimeID
	var receipt connectivity.ClockSample
	if !restore {
		receipt = connectivity.ClockSample{Wall: c.AcceptedAt, Epoch: c.Request.ClockEpoch, Uptime: c.Request.Uptime, UpperUptime: c.Request.UpperUptime}
	}
	return &connectionTarget{profile: p.ID, generation: c.Generation, intent: c.Intent, restore: restore, receipt: receipt}, nil
}

func (w *ConnectionWorker) prepareBudget(t *connectionTarget) error {
	if t.restore {
		sample, err := w.config.Clock.Sample()
		if err != nil {
			return connectivity.ErrClock
		}
		t.receipt = sample
	}
	if t.receipt.Epoch != w.config.RuntimeID {
		return connectivity.ErrClock
	}
	duration := 30 * time.Second
	if t.intent == state.IntentDisconnected {
		duration = 10 * time.Second
	}
	b, err := connectivity.NewOperationBudget(w.config.Clock, t.profile, t.generation, t.receipt, duration)
	if err != nil {
		return err
	}
	t.budget = b
	remaining, err := b.Remaining()
	if err != nil {
		return err
	}
	t.deadline = time.Now().Add(remaining)
	return nil
}

func sameConnectionTarget(a, b connectionTarget) bool {
	return a.profile == b.profile && a.generation == b.generation
}

func (w *ConnectionWorker) Reconcile(ctx context.Context, profile string, generation uint64, wait bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	t, err := w.target(ctx)
	if err != nil {
		return err
	}
	if t == nil || t.profile != profile || t.generation != generation {
		return state.ErrStale
	}
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return ErrConnectionWorker
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
	if !wait {
		return nil
	}
	for {
		w.mu.Lock()
		v, changed, running := w.view, w.changed, w.running
		w.mu.Unlock()
		if !running {
			return ErrConnectionWorker
		}
		if v.Profile == profile && v.Generation == generation {
			switch v.Phase {
			case ConnectionConnected, ConnectionDisconnected:
				current, err := w.target(ctx)
				if err != nil {
					return err
				}
				if current == nil || current.profile != profile || current.generation != generation {
					return state.ErrStale
				}
				return nil
			case ConnectionFailed:
				return v.Failure
			}
		} else if v.Profile != "" && (v.Profile != profile || v.Generation > generation) {
			return state.ErrStale
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func connectionOperationError(err error) error {
	var kind connectivity.FailureKind
	if errors.As(err, &kind) && kind != connectivity.FailureUnknown {
		return kind
	}
	switch {
	case errors.Is(err, connectivity.ErrUnavailable), errors.Is(err, connectivity.ErrEndpointUncertain):
		return connectivity.FailureControl
	case errors.Is(err, connectivity.ErrDenied), errors.Is(err, connectivity.ErrRevoked), errors.Is(err, connectivity.ErrExpired):
		return connectivity.FailureAccess
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, state.ErrStale):
		return state.ErrStale
	case errors.Is(err, connectivity.ErrClock):
		return connectivity.ErrClock
	default:
		return ErrConnectionOperation
	}
}

func (w *ConnectionWorker) setTarget(t *connectionTarget) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.observer = nil
	if t == nil {
		w.view = ConnectionObservation{}
	} else {
		phase := ConnectionConnecting
		if t.intent == state.IntentDisconnected {
			phase = ConnectionDisconnecting
		}
		w.view = ConnectionObservation{Profile: t.profile, Generation: t.generation, Intent: t.intent, Phase: phase, CleanupPending: true}
	}
	w.announceLocked()
}

func (w *ConnectionWorker) result(r connectionResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.view.Profile != r.job.target.profile || w.view.Generation != r.job.target.generation {
		return
	}
	if r.observer != nil {
		w.observer = r.observer
	}
	if r.err != nil && w.view.Phase != ConnectionFailed {
		w.view.Phase = ConnectionFailed
		w.view.Failure = connectionOperationError(r.err)
	} else if w.view.Phase != ConnectionFailed {
		if r.ready {
			w.view.Phase = ConnectionConnected
		} else if r.finished {
			w.view.Phase = ConnectionDisconnected
		}
	}
	if r.finished {
		w.view.CleanupPending = r.err != nil
		w.view.CleanupFailure = nil
		if r.err != nil {
			w.view.CleanupFailure = connectionOperationError(r.err)
		}
	}
	w.announceLocked()
}

func (w *ConnectionWorker) runJob(job *connectionJob, results chan<- connectionResult) {
	t := job.target
	opBase, cancelOperation := context.WithDeadline(job.lifetime, t.deadline)
	op := connectivity.WithOperationBudget(opBase, t.budget)
	defer cancelOperation()
	if job.previous != nil {
		err := connectivity.CheckOperation(op)
		if err == nil {
			err = job.previous.Disconnect(op)
		}
		if err == nil {
			err = connectivity.CheckOperation(op)
		}
		if err != nil {
			job.cancel()
			results <- connectionResult{job: job, finished: true, err: err, retained: job.previous}
			return
		}
	}
	session, err := w.factory(job.lifetime, t.profile, t.generation)
	if err == nil && session == nil {
		err = ErrConnectionOperation
	}
	if observer, ok := session.(connectivity.LocalObservationSource); ok {
		results <- connectionResult{job: job, observer: observer}
	}
	if err == nil && t.intent == state.IntentConnected {
		err = connectivity.CheckOperation(op)
		if err == nil {
			err = session.Connect(op, t.restore)
		}
		if err == nil {
			err = connectivity.CheckOperation(op)
		}
		if err == nil {
			results <- connectionResult{job: job, ready: true}
			cancelOperation()
			err = session.Serve(job.lifetime)
			if err == nil {
				err = ErrConnectionOperation
			}
		}
	}
	if t.intent == state.IntentDisconnected && err == nil {
		err = connectivity.CheckOperation(op)
		if err == nil {
			err = session.Disconnect(op)
		}
		if err == nil {
			err = connectivity.CheckOperation(op)
		}
		job.cancel()
		var retained ConnectionSession
		if err != nil {
			retained = session
		}
		results <- connectionResult{job: job, finished: true, err: err, retained: retained}
		return
	}
	// Cancellation revokes packet authority before potentially slow cleanup.
	job.cancel()
	results <- connectionResult{job: job, err: err}
	if session != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), w.config.CleanupTimeout)
		cleanupErr := session.Disconnect(cleanup)
		if cleanupErr == nil {
			cleanupErr = cleanup.Err()
		}
		cancel()
		// Keep the original failure, but cleanup failure must remain observable.
		if cleanupErr != nil {
			err = cleanupErr
		} else {
			err = nil
		}
	}
	var retained ConnectionSession
	if err != nil {
		retained = session
	}
	results <- connectionResult{job: job, finished: true, err: err, retained: retained}
}

func (w *ConnectionWorker) Run(ctx context.Context) (runErr error) {
	w.mu.Lock()
	if w.used || ctx.Err() != nil {
		w.mu.Unlock()
		return ErrConnectionWorker
	}
	w.used, w.running = true, true
	w.announceLocked()
	w.mu.Unlock()
	poll := time.NewTicker(w.config.PollInterval)
	defer poll.Stop()
	results := make(chan connectionResult, 3)
	var current *connectionJob
	var desired, pending *connectionTarget
	var deadlineTimer *time.Timer
	operationOpen := false
	defer func() {
		if deadlineTimer != nil {
			deadlineTimer.Stop()
		}
	}()
	// On shutdown no new owner is admitted. A misbehaving native operation cannot
	// make daemon shutdown unbounded or cause a second concurrent owner.
	defer func() {
		w.mu.Lock()
		w.running = false
		if w.view.Profile != "" && w.view.Phase != ConnectionFailed {
			w.view.Phase = ConnectionFailed
			w.view.Failure = connectionOperationError(runErr)
		}
		w.announceLocked()
		w.mu.Unlock()
		if current == nil {
			if w.retired != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), w.config.CleanupTimeout)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- w.retired.Disconnect(cleanup) }()
				select {
				case err := <-done:
					if err == nil {
						err = cleanup.Err()
					}
					if err != nil {
						runErr = errors.Join(runErr, ErrLifecycle)
					} else {
						w.retired = nil
						w.mu.Lock()
						w.view.CleanupPending = false
						w.announceLocked()
						w.mu.Unlock()
					}
				case <-cleanup.Done():
					runErr = errors.Join(runErr, ErrLifecycle)
				}
			}
			return
		}
		current.cancel()
		bound := time.NewTimer(w.config.CleanupTimeout)
		defer bound.Stop()
		for {
			select {
			case r := <-results:
				w.result(r)
				if r.job == current && r.finished {
					w.retired = r.retained
					if r.err != nil {
						runErr = errors.Join(runErr, ErrLifecycle)
					}
					return
				}
			case <-bound.C:
				runErr = errors.Join(runErr, ErrLifecycle)
				return
			}
		}
	}()
	load := true
	for {
		if operationOpen && desired != nil {
			if err := desired.budget.Check(); err != nil {
				if current != nil {
					current.cancel()
				}
				pending = nil
				operationOpen = false
				if deadlineTimer != nil {
					deadlineTimer.Stop()
				}
				w.result(connectionResult{job: &connectionJob{target: *desired}, err: err})
			}
		}
		if load {
			readCtx, cancel := context.WithCancel(ctx)
			if operationOpen && desired != nil {
				cancel()
				readCtx, cancel = context.WithDeadline(ctx, desired.deadline)
			}
			t, err := w.target(readCtx)
			cancel()
			if err != nil {
				if operationOpen && desired != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) && !time.Now().Before(desired.deadline) {
					if current != nil {
						current.cancel()
					}
					pending = nil
					operationOpen = false
					load = false
					w.result(connectionResult{job: &connectionJob{target: *desired}, err: context.DeadlineExceeded})
					continue
				}
				if current != nil {
					current.cancel()
				}
				return connectionOperationError(err)
			}
			if t == nil && desired != nil || t != nil && (desired == nil || !sameConnectionTarget(*t, *desired)) {
				desired, pending = t, t
				w.setTarget(t)
				if deadlineTimer != nil {
					deadlineTimer.Stop()
					deadlineTimer = nil
				}
				operationOpen = t != nil
				if t != nil {
					if err := w.prepareBudget(t); err != nil {
						pending = nil
						operationOpen = false
						w.result(connectionResult{job: &connectionJob{target: *t}, err: err})
					} else {
						deadlineTimer = time.NewTimer(time.Until(t.deadline))
					}
				}
				if current != nil {
					current.cancel()
				}
			}
			load = false
		}
		if current == nil && pending != nil {
			t := *pending
			pending = nil
			lifetime, cancel := context.WithCancel(ctx)
			current = &connectionJob{target: t, lifetime: lifetime, cancel: cancel, previous: w.retired}
			w.retired = nil
			go w.runJob(current, results)
		}
		var timeout <-chan time.Time
		if deadlineTimer != nil {
			timeout = deadlineTimer.C
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
			load = true
		case <-poll.C:
			load = true
		case <-timeout:
			if current != nil {
				current.cancel()
			}
			pending = nil
			operationOpen = false
			w.result(connectionResult{job: &connectionJob{target: *desired}, err: context.DeadlineExceeded})
		case r := <-results:
			if current != r.job {
				continue
			}
			if r.ready {
				if err := current.target.budget.Check(); err != nil {
					r.ready = false
					r.err = err
					current.cancel()
				} else if current.lifetime.Err() != nil {
					r.ready = false
					r.err = current.lifetime.Err()
				} else {
					check, cancel := context.WithDeadline(ctx, current.target.deadline)
					latest, err := w.target(check)
					cancel()
					if err != nil || latest == nil || !sameConnectionTarget(*latest, current.target) {
						r.ready = false
						if err != nil {
							r.err = err
						} else {
							r.err = state.ErrStale
						}
						current.cancel()
						load = true
					} else if err := current.target.budget.Complete(); err != nil {
						r.ready = false
						r.err = err
						current.cancel()
					} else {
						operationOpen = false
						if deadlineTimer != nil {
							deadlineTimer.Stop()
						}
					}
				}
			}
			w.result(r)
			if r.finished {
				w.retired = r.retained
				current.cancel()
				if desired != nil && sameConnectionTarget(*desired, current.target) && deadlineTimer != nil {
					deadlineTimer.Stop()
					operationOpen = false
				}
				current = nil
				load = true
			}
		}
	}
}
