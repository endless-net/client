package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var ErrLoginQueueFull = errors.New("native login queue full")
var ErrLoginPhaseUnsupported = errors.New("login worker does not implement admission")
var ErrLoginWorkerRunning = errors.New("native login worker already running")

type loginJob struct {
	profile, attempt string
	due              time.Time
	generation       uint64
	cause            trace.SpanContext
	recovered        bool
}
type LoginWorker struct {
	executor      *LoginExecutor
	registration  *RegistrationExecutor
	approval      *ApprovalExecutor
	defaultTarget *DefaultTargetExecutor
	mu, run       sync.Mutex
	jobs          map[string]loginJob
	notify        chan struct{}
	telemetry     *workerTelemetry
}

// Construction starts no goroutine. The application owns Run and its lifetime.
func NewLoginWorker(executor *LoginExecutor, tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*LoginWorker, error) {
	if executor == nil {
		return nil, ErrNativeProtocol
	}
	telemetry, err := newWorkerTelemetry(tracer, meter, logger)
	if err != nil {
		return nil, err
	}
	return &LoginWorker{executor: executor, jobs: map[string]loginJob{}, notify: make(chan struct{}, 1), telemetry: telemetry}, nil
}

// NewEnrollmentWorker explicitly enables the published direct-registration
// adapter. Login-only construction retains its narrower phase boundary.
func NewEnrollmentWorker(login *LoginExecutor, registration *RegistrationExecutor, tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*LoginWorker, error) {
	if login == nil || registration == nil || login.store != registration.store {
		return nil, ErrRegistrationProtocol
	}
	w, err := NewLoginWorker(login, tracer, meter, logger)
	if err != nil {
		return nil, err
	}
	w.registration = registration
	return w, nil
}

// NewApprovalEnrollmentWorker explicitly enables the already-persisted browser
// class. It selects no UI/IPC entrypoint and performs no construction-time I/O.
func NewApprovalEnrollmentWorker(login *LoginExecutor, registration *RegistrationExecutor, approval *ApprovalExecutor, tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*LoginWorker, error) {
	if approval == nil || login == nil || approval.store != login.store {
		return nil, approvalmodel.ErrProtocol
	}
	w, err := NewEnrollmentWorker(login, registration, tracer, meter, logger)
	if err != nil {
		return nil, err
	}
	w.approval = approval
	return w, nil
}

// NewDefaultEnrollmentWorker enables server-owned default resolution explicitly.
func NewDefaultEnrollmentWorker(login *LoginExecutor, registration *RegistrationExecutor, approval *ApprovalExecutor, target *DefaultTargetExecutor, tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*LoginWorker, error) {
	if target == nil || login == nil || target.store != login.store {
		return nil, ErrDefaultTargetProtocol
	}
	w, err := NewApprovalEnrollmentWorker(login, registration, approval, tracer, meter, logger)
	if err != nil {
		return nil, err
	}
	w.defaultTarget = target
	return w, nil
}

func (w *LoginWorker) supports(p state.Profile) bool {
	if p.Attempt == nil {
		return false
	}
	if p.Attempt.BrowserApproval {
		return w.approval != nil && (p.Attempt.Phase == enrollment.PendingApproval || p.Attempt.Phase == enrollment.Completing)
	}
	return p.Attempt.Phase == enrollment.AwaitingLogin || p.Attempt.Phase == enrollment.Registering && w.registration != nil || p.Attempt.Phase == enrollment.ChoosingContext && w.defaultTarget != nil
}

func (w *LoginWorker) startable(p state.Profile) bool {
	if !w.supports(p) {
		return false
	}
	if p.Attempt.BrowserApproval {
		return approvalActive(p)
	}
	if p.Attempt.Phase == enrollment.AwaitingLogin {
		return p.Login.BlockedReason == ""
	}
	if p.Attempt.Phase == enrollment.ChoosingContext {
		return p.DefaultTarget == nil || p.DefaultTarget.BlockedReason != "client.default_target.invalid_response"
	}
	return p.Registration.BlockedReason != "client.registration.invalid_response"
}
func (w *LoginWorker) Wake(ctx context.Context, profile, attempt string) error {
	return w.wake(ctx, profile, attempt, false)
}

func (w *LoginWorker) wake(ctx context.Context, profile, attempt string, recovered bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !identifier(profile) || !identifier(attempt) {
		return ErrNativeProtocol
	}
	s, err := w.executor.store.Load(ctx)
	if err != nil {
		return err
	}
	p, ok := s.Profiles[profile]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt {
		return state.ErrStale
	}
	if p.Attempt.Phase == enrollment.ChoosingContext && w.defaultTarget == nil {
		return nil
	}
	if !w.supports(p) {
		return ErrLoginPhaseUnsupported
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing, ok := w.jobs[profile]; ok {
		if existing.attempt != attempt {
			w.telemetry.queued(ctx, "stale", len(w.jobs))
			return state.ErrStale
		}
		if existing.generation == ^uint64(0) {
			w.telemetry.queued(ctx, "stale", len(w.jobs))
			return state.ErrStale
		}
		if recovered {
			return nil
		} // Preserve a pre-Run explicit Wake's cause.
		existing.generation++
		existing.due = w.executor.config.Now()
		existing.cause, existing.recovered = jobCause(ctx), false
		w.jobs[profile] = existing
		w.telemetry.queued(ctx, "coalesced", len(w.jobs))
		select {
		case w.notify <- struct{}{}:
		default:
		}
		return nil
	}
	if len(w.jobs) >= w.executor.config.MaxPending {
		w.telemetry.queued(ctx, "full", len(w.jobs))
		return ErrLoginQueueFull
	}
	cause := jobCause(ctx)
	if recovered {
		cause = trace.SpanContext{}
	}
	w.jobs[profile] = loginJob{profile: profile, attempt: attempt, due: w.executor.config.Now(), generation: 1, cause: cause, recovered: recovered}
	w.telemetry.queued(ctx, "accepted", len(w.jobs))
	select {
	case w.notify <- struct{}{}:
	default:
	}
	return nil
}

func (w *LoginWorker) Run(ctx context.Context) error {
	if !w.run.TryLock() {
		return ErrLoginWorkerRunning
	}
	defer w.run.Unlock()
	s, err := w.executor.store.Load(ctx)
	if err != nil {
		return err
	}
	for id, p := range s.Profiles {
		if w.startable(p) && p.Trust.KeyID != "" {
			if err = w.wake(ctx, id, p.Attempt.AttemptID, true); err != nil {
				return err
			}
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.mu.Lock()
		var selected loginJob
		for _, job := range w.jobs {
			if selected.profile == "" || job.due.Before(selected.due) {
				selected = job
			}
		}
		w.mu.Unlock()
		if selected.profile == "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-w.notify:
			}
			continue
		}
		wait := selected.due.Sub(w.executor.config.Now())
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-w.notify:
				timer.Stop()
				continue
			case <-timer.C:
			}
		}
		jobCtx, finish := w.telemetry.start(ctx, selected)
		due, err := w.reconcile(jobCtx, selected)
		finish(due, err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, state.ErrStale) || errors.Is(err, state.ErrUncertain) {
			due = w.executor.config.Now().Add(w.executor.config.RetryInterval)
		} else if err != nil {
			return err
		}
		w.mu.Lock()
		if current, ok := w.jobs[selected.profile]; ok && current.generation != selected.generation {
			// A new phase's Wake may arrive while the prior call is finishing.
			// Preserve it instead of deleting the next registration job.
			w.mu.Unlock()
			continue
		}
		if due.IsZero() {
			delete(w.jobs, selected.profile)
			w.telemetry.depth.Record(ctx, int64(len(w.jobs)))
		} else {
			selected.due = due
			w.jobs[selected.profile] = selected
		}
		w.mu.Unlock()
	}
}

func (w *LoginWorker) reconcile(ctx context.Context, job loginJob) (time.Time, error) {
	if w.registration != nil || w.approval != nil {
		s, err := w.executor.store.Load(ctx)
		if err != nil {
			return time.Time{}, err
		}
		p, ok := s.Profiles[job.profile]
		if ok && p.Attempt != nil && p.Attempt.AttemptID == job.attempt && p.Attempt.BrowserApproval {
			if w.approval == nil {
				return time.Time{}, ErrLoginPhaseUnsupported
			}
			return w.approval.Reconcile(ctx, job.profile, job.attempt)
		}
		if ok && p.Attempt != nil && p.Attempt.AttemptID == job.attempt && p.Attempt.Phase == enrollment.Registering {
			return w.registration.Reconcile(ctx, job.profile, job.attempt)
		}
		if ok && p.Attempt != nil && p.Attempt.AttemptID == job.attempt && p.Attempt.Phase == enrollment.ChoosingContext && w.defaultTarget != nil {
			return w.defaultTarget.Reconcile(ctx, job.profile, job.attempt)
		}
	}
	due, err := w.executor.Reconcile(ctx, job.profile, job.attempt)
	if err == nil && due.IsZero() && w.defaultTarget != nil {
		s, readErr := w.executor.store.Load(ctx)
		if readErr != nil {
			return time.Time{}, readErr
		}
		p := s.Profiles[job.profile]
		if p.Attempt != nil && p.Attempt.AttemptID == job.attempt && p.Attempt.Phase == enrollment.ChoosingContext {
			return w.executor.config.Now(), nil
		}
	}
	return due, err
}
