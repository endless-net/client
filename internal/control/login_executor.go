package control

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	identityapi "github.com/endless-net/identity/api/v1"
)

type LoginConfig struct {
	MaxPending, MaxCommitRetries int
	RetryInterval, WriteBudget   time.Duration
	Now                          func() time.Time
}
type LoginExecutor struct {
	run    sync.Mutex
	store  *state.Store
	native *Native
	config LoginConfig
}

func NewLoginExecutor(store *state.Store, native *Native, config LoginConfig) (*LoginExecutor, error) {
	if store == nil || native == nil || config.MaxPending <= 0 || config.MaxCommitRetries <= 0 || config.RetryInterval <= 0 || config.RetryInterval > 30*time.Second || config.WriteBudget <= 0 || config.WriteBudget > 30*time.Second {
		return nil, ErrNativeProtocol
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &LoginExecutor{store: store, native: native, config: config}, nil
}

// Reconcile performs at most one remote native-login call. It never generates
// keys/request IDs/proofs or registers a Node. A zero due time pauses this phase.
func (e *LoginExecutor) Reconcile(ctx context.Context, id, attempt string) (time.Time, error) {
	if !e.run.TryLock() {
		return time.Time{}, state.ErrStale
	}
	defer e.run.Unlock()
	s, err := e.store.Load(ctx)
	if err != nil {
		return time.Time{}, err
	}
	p, ok := s.Profiles[id]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt {
		return time.Time{}, nil // Obsolete work cannot recreate a removed attempt.
	}
	now := e.config.Now()
	a := *p.Attempt
	if p.RemoteInFlight && !a.Uncertain {
		a, err = a.RecoverLostCall(p.InFlightAt, now)
	} else {
		a, err = a.Observe(now)
	}
	if err != nil {
		return time.Time{}, err
	}
	if a != *p.Attempt {
		err = e.store.Update(ctx, s.Revision, func(s *state.State) error { p.Attempt = &a; s.Profiles[id] = p; return nil })
		if err != nil {
			return time.Time{}, err
		}
		s.Revision++
		p.Attempt = &a
	}
	if a.Phase != enrollment.AwaitingLogin || p.Login.BlockedReason != "" || p.Trust.KeyID == "" {
		return time.Time{}, nil
	}
	if now.Before(p.Login.NextPollAt) {
		return p.Login.NextPollAt, nil
	}
	// The original request proof/key/binding already exist. Mark the send durably
	// before bootstrap/RPC; a crash cannot manufacture a new recovery budget.
	if !p.RemoteInFlight {
		p.RemoteInFlight = true
		p.InFlightAt = now
	}
	err = e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil })
	if err != nil {
		return time.Time{}, err
	}
	t, poll, callErr := e.native.Call(ctx, p, false)
	// A shutdown may have cancelled the call after its remote effect. Bound a
	// final local write independently; if it fails, persisted InFlightAt survives.
	saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), e.config.WriteBudget)
	defer stop()
	for range e.config.MaxCommitRetries {
		current, err := e.store.Load(saveCtx)
		if err != nil {
			return time.Time{}, err
		}
		latest, ok := current.Profiles[id]
		if !ok || latest.Attempt == nil || latest.Attempt.Binding != p.Attempt.Binding {
			return time.Time{}, state.ErrStale
		}
		next, err := e.apply(latest, t, poll, callErr)
		if err != nil {
			return time.Time{}, err
		}
		err = e.store.Update(saveCtx, current.Revision, func(s *state.State) error { s.Profiles[id] = next; return nil })
		if errors.Is(err, state.ErrStale) {
			continue
		}
		if err != nil {
			return time.Time{}, err
		}
		if next.Attempt.Phase != enrollment.AwaitingLogin || next.Login.BlockedReason != "" {
			return time.Time{}, nil
		}
		return next.Login.NextPollAt, nil
	}
	return time.Time{}, state.ErrStale
}

func (e *LoginExecutor) apply(p state.Profile, t *identityapi.NativeLoginTransaction, poll string, callErr error) (state.Profile, error) {
	now := e.config.Now()
	a, err := p.Attempt.Observe(now)
	if err != nil {
		return p, err
	}
	if callErr != nil {
		if errors.Is(callErr, ErrBootstrap) {
			p.RemoteInFlight = false
			p.InFlightAt = time.Time{}
		} else {
			if a.Phase == enrollment.Expired || a.Phase == enrollment.Cancelled {
				a, err = a.RecoverLostCall(p.InFlightAt, now)
			} else {
				a, err = (*p.Attempt).MarkUncertain(now)
			}
			if err != nil {
				return p, err
			}
			code := connect.CodeOf(callErr)
			if errors.Is(callErr, ErrNativeProtocol) || (code != connect.CodeUnavailable && code != connect.CodeDeadlineExceeded && code != connect.CodeCanceled && code != connect.CodeUnknown && code != connect.CodeInternal) {
				p.Login.BlockedReason = "client.login.invalid_response"
			}
		}
		p.Attempt = &a
		p.Login.VerificationURL = ""
		p.Login.NextPollAt = nextCall(a, now.Add(e.config.RetryInterval))
		return p, nil
	}
	p.RemoteInFlight = false
	p.InFlightAt = time.Time{}
	p.Secrets.TransactionID = t.TransactionId
	p.Secrets.PollProof = poll
	p.Login.CreatedAt = t.CreatedAt.AsTime()
	p.Login.ExpiresAt = t.ExpiresAt.AsTime()
	p.Login.ReplayUntil = t.ReplayExpiresAt.AsTime()
	p.Login.VerificationURL = ""
	if a.Phase != enrollment.AwaitingLogin {
		// Cancel/expiry won locally. No native grant is persisted or forwarded.
		p.Attempt = &a
		return p, nil
	}
	if a.Uncertain {
		a.Uncertain = false
		a.Revision++
	}
	switch t.State {
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING:
		p.Login.VerificationURL = t.GetBrowserAction().VerificationUrl
		p.Login.NextPollAt = nextCall(a, now.Add(time.Duration(t.PollAfterSeconds)*time.Second))
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED:
		a, err = a.LoginSucceeded(now)
		if err != nil {
			return p, err
		}
		r := t.GetResult()
		p.Secrets.Access = r.AccessBearer
		p.Secrets.Renewal = r.RenewalAuthorization.Bearer
		p.Secrets.SessionID = r.Session.SessionId
		p.Secrets.UserID = r.Session.UserId
		p.Secrets.SessionExpiresAt = r.Session.ExpiresAt.AsTime()
		p.Secrets.RenewalExpiresAt = r.RenewalAuthorization.ExpiresAt.AsTime()
		p.Login.NextPollAt = time.Time{}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_DENIED:
		a, err = a.Reject(now)
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_CANCELLED:
		a, err = a.Cancel(now)
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_EXPIRED:
		a.Phase = enrollment.Expired
		a.Revision++
	default:
		return p, ErrNativeProtocol
	}
	p.Attempt = &a
	return p, err
}

func nextCall(a enrollment.Attempt, due time.Time) time.Time {
	for _, deadline := range []time.Time{a.LoginUntil, a.Deadline} {
		if deadline.Before(due) {
			due = deadline
		}
	}
	if a.Uncertain && a.RecoveryUntil.Before(due) {
		due = a.RecoveryUntil
	}
	return due
}
