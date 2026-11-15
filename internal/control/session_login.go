package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	identityapi "github.com/endless-net/identity/api/v1"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

type SessionLoginWorker struct {
	store            *state.Store
	native           *Native
	config           LoginConfig
	telemetry        *workerTelemetry
	mu, run, execute sync.Mutex
	jobs             map[string]loginJob
	paused           map[string]string
	notify           chan struct{}
}

func NewSessionLoginWorker(store *state.Store, native *Native, config LoginConfig, tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*SessionLoginWorker, error) {
	validated, err := NewLoginExecutor(store, native, config)
	if err != nil {
		return nil, err
	}
	telemetry, err := newNamedWorkerTelemetry(tracer, meter, logger, "session_login")
	if err != nil {
		return nil, err
	}
	return &SessionLoginWorker{store: store, native: native, config: validated.config, telemetry: telemetry, jobs: map[string]loginJob{}, paused: map[string]string{}, notify: make(chan struct{}, 1)}, nil
}

func (w *SessionLoginWorker) Wake(ctx context.Context, profile, attempt string) error {
	return w.wake(ctx, profile, attempt, false)
}

func (w *SessionLoginWorker) wake(ctx context.Context, profile, attempt string, recovered bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !identifier(profile) || !identifier(attempt) {
		return ErrNativeProtocol
	}
	s, err := w.store.Load(ctx)
	if err != nil {
		return err
	}
	p, ok := s.Profiles[profile]
	if !ok || p.SessionLogin == nil || p.SessionLogin.Binding.AttemptID != attempt {
		return state.ErrStale
	}
	l := p.SessionLogin
	if l.Terminal() || l.Phase == state.SessionLoginUncertain && (!w.config.Now().Before(l.EffectiveDeadline()) || !l.RecoveryUntil.IsZero() && !w.config.Now().Before(l.RecoveryUntil)) {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.paused[profile] == attempt {
		return state.ErrCapacity
	}
	delete(w.paused, profile)
	job, exists := w.jobs[profile]
	if exists && job.attempt == attempt {
		return nil
	}
	if !exists && len(w.jobs) >= w.config.MaxPending {
		w.telemetry.queued(ctx, "full", len(w.jobs))
		return ErrLoginQueueFull
	}
	due := w.config.Now()
	if l.NextPollAt.After(due) {
		due = l.NextPollAt
	}
	w.jobs[profile] = loginJob{profile: profile, attempt: attempt, due: due, generation: job.generation + 1, cause: jobCause(ctx), recovered: recovered}
	w.telemetry.queued(ctx, "accepted", len(w.jobs))
	select {
	case w.notify <- struct{}{}:
	default:
	}
	return nil
}

func (w *SessionLoginWorker) recover(ctx context.Context) error {
	s, err := w.store.Load(ctx)
	if err != nil {
		return err
	}
	for id, p := range s.Profiles {
		if p.SessionLogin != nil && !p.SessionLogin.Terminal() {
			if err := w.wake(ctx, id, p.SessionLogin.Binding.AttemptID, true); err != nil && !errors.Is(err, ErrLoginQueueFull) && !errors.Is(err, state.ErrStale) && !errors.Is(err, state.ErrCapacity) {
				return err
			}
		}
	}
	return nil
}

func (w *SessionLoginWorker) Run(ctx context.Context) error {
	if !w.run.TryLock() {
		return ErrLoginWorkerRunning
	}
	defer w.run.Unlock()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := w.recover(ctx); err != nil {
			return err
		}
		w.mu.Lock()
		var selected loginJob
		for _, job := range w.jobs {
			if selected.profile == "" || job.due.Before(selected.due) {
				selected = job
			}
		}
		w.mu.Unlock()
		wait := w.config.RetryInterval
		if selected.profile != "" {
			wait = min(wait, max(0, selected.due.Sub(w.config.Now())))
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-w.notify:
				timer.Stop()
			case <-timer.C:
			}
			continue
		}
		jobCtx, finish := w.telemetry.start(ctx, selected)
		due, err := w.Reconcile(jobCtx, selected.profile, selected.attempt)
		finish(due, err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, state.ErrCapacity) {
			w.mu.Lock()
			w.paused[selected.profile] = selected.attempt
			delete(w.jobs, selected.profile)
			w.mu.Unlock()
			continue
		}
		if errors.Is(err, state.ErrStale) || errors.Is(err, state.ErrUncertain) || errors.Is(err, context.DeadlineExceeded) {
			due = w.config.Now().Add(w.config.RetryInterval)
		} else if err != nil {
			return err
		}
		w.mu.Lock()
		if current, ok := w.jobs[selected.profile]; ok && current.generation == selected.generation {
			if due.IsZero() {
				delete(w.jobs, selected.profile)
			} else {
				selected.due = due
				w.jobs[selected.profile] = selected
			}
		}
		w.telemetry.depth.Record(ctx, int64(len(w.jobs)))
		w.mu.Unlock()
	}
}

func (w *SessionLoginWorker) Reconcile(ctx context.Context, profile, attempt string) (time.Time, error) {
	if !w.execute.TryLock() {
		return time.Time{}, state.ErrStale
	}
	defer w.execute.Unlock()
	s, err := w.store.Load(ctx)
	if err != nil {
		return time.Time{}, err
	}
	p, ok := s.Profiles[profile]
	if !ok || p.SessionLogin == nil || p.SessionLogin.Binding.AttemptID != attempt || p.SessionLogin.Terminal() {
		return time.Time{}, nil
	}
	l := p.SessionLogin
	now := w.config.Now().UTC()
	if now.Before(l.ObservedAt) {
		return time.Time{}, state.ErrCorrupt
	}
	if !now.Before(l.EffectiveDeadline()) || l.Phase == state.SessionLoginUncertain && !l.RecoveryUntil.IsZero() && !now.Before(l.RecoveryUntil) {
		l.ObservedAt = now
		l.NextPollAt = time.Time{}
		if !l.InFlightAt.IsZero() || l.Phase == state.SessionLoginUncertain {
			l.Phase = state.SessionLoginUncertain
			l.Reason = state.SessionLoginOutcomeUnknown
		} else {
			l.Phase = state.SessionLoginExpired
			l.Reason = state.SessionLoginDeadlineExceeded
		}
		return time.Time{}, w.store.Update(ctx, s.Revision, func(value *state.State) error { value.Profiles[profile] = p; return nil })
	}
	if now.Before(l.NextPollAt) {
		return l.NextPollAt, nil
	}
	// A persisted in-flight marker at entry survived a lost reply or process exit.
	if !l.InFlightAt.IsZero() {
		if l.RecoveryUntil.IsZero() {
			l.RecoveryUntil = minLoginTime(l.InFlightAt.Add(enrollment.RecoveryTTL), l.Deadline)
		}
		l.Phase = state.SessionLoginUncertain
		l.Reason = state.SessionLoginRetrying
		if !now.Before(l.RecoveryUntil) {
			l.Reason = state.SessionLoginOutcomeUnknown
			l.ObservedAt = now
			l.NextPollAt = time.Time{}
			return time.Time{}, w.store.Update(ctx, s.Revision, func(value *state.State) error { value.Profiles[profile] = p; return nil })
		}
	} else {
		l.InFlightAt = now
	}
	if l.SentAt.IsZero() {
		l.SentAt = now
	}
	l.ObservedAt = now
	l.NextPollAt = time.Time{}
	if err := w.store.Update(ctx, s.Revision, func(value *state.State) error { value.Profiles[profile] = p; return nil }); err != nil {
		return time.Time{}, err
	}
	shadow, err := p.SessionLoginShadow()
	if err != nil {
		return time.Time{}, err
	}
	deadline := l.EffectiveDeadline()
	if l.Phase == state.SessionLoginUncertain && !l.RecoveryUntil.IsZero() {
		deadline = minLoginTime(deadline, l.RecoveryUntil)
	}
	call, stop := context.WithTimeout(ctx, deadline.Sub(w.config.Now()))
	tx, poll, callErr := w.native.Call(call, shadow, false)
	stop()
	save, stopSave := context.WithTimeout(context.WithoutCancel(ctx), w.config.WriteBudget)
	defer stopSave()
	for range w.config.MaxCommitRetries {
		current, err := w.store.Load(save)
		if err != nil {
			return time.Time{}, err
		}
		latest, ok := current.Profiles[profile]
		if !ok || latest.SessionLogin == nil || latest.SessionLogin.Binding != l.Binding || latest.SessionLogin.Terminal() {
			return time.Time{}, nil
		}
		next, err := w.apply(latest, tx, poll, callErr)
		if err != nil {
			return time.Time{}, err
		}
		err = w.store.Update(save, current.Revision, func(value *state.State) error { value.Profiles[profile] = next; return nil })
		if errors.Is(err, state.ErrStale) {
			continue
		}
		if errors.Is(err, state.ErrCapacity) {
			// Keep original credentials/operation, reject the unpersistable result.
			latest.SessionLogin.Phase = state.SessionLoginFailed
			latest.SessionLogin.Reason = state.SessionLoginCapacity
			latest.SessionLogin.ObservedAt = w.config.Now().UTC()
			latest.SessionLogin.NextPollAt = time.Time{}
			latest.SessionLogin.InFlightAt = time.Time{}
			err = w.store.Update(save, current.Revision, func(value *state.State) error { value.Profiles[profile] = latest; return nil })
			return time.Time{}, err
		}
		if err != nil {
			return time.Time{}, err
		}
		return next.SessionLogin.NextPollAt, nil
	}
	return time.Time{}, state.ErrStale
}

func minLoginTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (w *SessionLoginWorker) apply(p state.Profile, tx *identityapi.NativeLoginTransaction, poll string, callErr error) (state.Profile, error) {
	l := p.SessionLogin
	// Copy mutable operation state: a failed result save must retain its predecessor.
	copyLogin := *l
	copyLogin.Transaction = append([]byte(nil), l.Transaction...)
	p.SessionLogin = &copyLogin
	l = &copyLogin
	now := w.config.Now().UTC()
	l.ObservedAt = now
	l.NextPollAt = time.Time{}
	if !now.Before(l.EffectiveDeadline()) {
		l.Phase = state.SessionLoginUncertain
		l.Reason = state.SessionLoginOutcomeUnknown
		return p, nil
	}
	if callErr != nil {
		if errors.Is(callErr, ErrBootstrap) {
			l.InFlightAt = time.Time{}
			l.Reason = state.SessionLoginControlUnavailable
			if len(l.Transaction) == 0 {
				l.Phase = state.SessionLoginPreparing
			} else {
				l.Phase = state.SessionLoginPending
			}
			l.NextPollAt = minLoginTime(now.Add(w.config.RetryInterval), l.EffectiveDeadline())
			return p, nil
		}
		code := connect.CodeOf(callErr)
		if errors.Is(callErr, ErrNativeProtocol) || (code != connect.CodeUnavailable && code != connect.CodeDeadlineExceeded && code != connect.CodeCanceled && code != connect.CodeUnknown && code != connect.CodeInternal) {
			l.Phase = state.SessionLoginFailed
			l.Reason = state.SessionLoginInvalidResponse
			if code == connect.CodeUnauthenticated || code == connect.CodePermissionDenied {
				l.Reason = state.SessionLoginAccessDenied
			}
			return p, nil
		}
		l.Phase = state.SessionLoginUncertain
		l.Reason = state.SessionLoginRetrying
		if l.RecoveryUntil.IsZero() {
			l.RecoveryUntil = minLoginTime(l.InFlightAt.Add(enrollment.RecoveryTTL), l.Deadline)
		}
		if !now.Before(l.RecoveryUntil) {
			l.Reason = state.SessionLoginOutcomeUnknown
			return p, nil
		}
		l.NextPollAt = minLoginTime(now.Add(w.config.RetryInterval), minLoginTime(l.EffectiveDeadline(), l.RecoveryUntil))
		return p, nil
	}
	if tx == nil {
		return p, ErrNativeProtocol
	}
	if tx.GetResult() != nil && tx.GetResult().Session.UserId != p.Secrets.UserID {
		l.Phase = state.SessionLoginFailed
		l.Reason = state.SessionLoginForeignUser
		l.InFlightAt = time.Time{}
		return p, nil
	}
	raw, err := proto.Marshal(tx)
	if err != nil {
		return p, ErrNativeProtocol
	}
	if p.CheckSessionLoginTransaction(raw) != nil {
		l.Phase = state.SessionLoginFailed
		l.Reason = state.SessionLoginInvalidResponse
		return p, nil
	}
	previousRaw, previousPoll := l.Transaction, l.PollProof
	l.Transaction = raw
	l.PollProof = poll
	l.InFlightAt = time.Time{}
	l.Reason = ""
	switch tx.State {
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING:
		if !now.Before(l.EffectiveDeadline()) {
			l.Phase = state.SessionLoginExpired
			l.Reason = state.SessionLoginDeadlineExceeded
			return p, nil
		}
		l.Phase = state.SessionLoginPending
		l.NextPollAt = minLoginTime(now.Add(time.Duration(tx.PollAfterSeconds)*time.Second), l.EffectiveDeadline())
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED:
		if !now.Before(l.EffectiveDeadline()) || !now.Before(tx.GetResult().Session.ExpiresAt.AsTime()) || !now.Before(tx.GetResult().RenewalAuthorization.ExpiresAt.AsTime()) {
			l.Transaction, l.PollProof = previousRaw, previousPoll
			l.Phase = state.SessionLoginFailed
			l.Reason = state.SessionLoginDeadlineExceeded
			return p, nil
		}
		previous, err := p.CurrentSession()
		if err != nil {
			return p, err
		}
		if tx.GetResult().Session.SessionId == previous.SessionID || tx.GetResult().AccessBearer == previous.Access || tx.GetResult().RenewalAuthorization.Bearer == previous.Renewal {
			l.Transaction, l.PollProof = previousRaw, previousPoll
			l.Phase = state.SessionLoginFailed
			l.Reason = state.SessionLoginInvalidResponse
			return p, nil
		}
		l.Phase = state.SessionLoginSucceeded
		p.ActiveSession = &state.ActiveSession{RequestID: l.Binding.RequestID, PreviousSessionID: previous.SessionID, PreviousRenewalSHA256: state.CredentialDigest(previous.Renewal), Native: &state.NativeSession{Binding: l.Binding, CreatedAt: l.CreatedAt, AdoptedAt: now, Transaction: append([]byte(nil), raw...)}}
		if p.SessionRefresh != nil {
			history := make(map[string]*state.SessionRefresh, len(p.SessionRefreshHistory)+1)
			for id, r := range p.SessionRefreshHistory {
				history[id] = r
			}
			history[p.SessionRefresh.RequestID] = p.SessionRefresh
			p.SessionRefreshHistory = history
			p.SessionRefresh = nil
		}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_DENIED, identityapi.NativeLoginState_NATIVE_LOGIN_STATE_CANCELLED:
		l.Phase = state.SessionLoginDenied
		l.Reason = state.SessionLoginRejected
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_EXPIRED:
		l.Phase = state.SessionLoginExpired
		l.Reason = state.SessionLoginDeadlineExceeded
	default:
		return p, ErrNativeProtocol
	}
	return p, nil
}
