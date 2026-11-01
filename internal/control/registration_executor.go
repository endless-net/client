package control

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

type RegistrationExecutorConfig struct {
	MaxCommitRetries           int
	RetryInterval, WriteBudget time.Duration
	Now                        func() time.Time
}

type RegistrationExecutor struct {
	run    sync.Mutex
	store  *state.Store
	remote *Registration
	config RegistrationExecutorConfig
}

func NewRegistrationExecutor(store *state.Store, remote *Registration, config RegistrationExecutorConfig) (*RegistrationExecutor, error) {
	if store == nil || remote == nil || config.MaxCommitRetries <= 0 || config.RetryInterval <= 0 || config.RetryInterval > 30*time.Second || config.WriteBudget <= 0 || config.WriteBudget > 30*time.Second {
		return nil, ErrRegistrationProtocol
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &RegistrationExecutor{store: store, remote: remote, config: config}, nil
}

// Reconcile performs at most one direct registration call. It persists the exact
// request and send marker together before dispatch; restart reuses those bytes.
// It never switches to an approval operation or generates an ID/key/session.
func (e *RegistrationExecutor) Reconcile(ctx context.Context, id, attempt string) (time.Time, error) {
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
		return time.Time{}, nil
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
	p.Attempt = &a
	if a != *s.Profiles[id].Attempt {
		if err = e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil }); err != nil {
			return time.Time{}, err
		}
		s.Revision++
	}
	if a.Phase != enrollment.Registering || p.Trust.KeyID == "" || p.Registration.BlockedReason == "client.registration.invalid_response" {
		return time.Time{}, nil
	}
	if now.Before(p.Registration.NextCallAt) {
		return p.Registration.NextCallAt, nil
	}
	original, err := PrepareRegistration(p)
	if err != nil {
		return time.Time{}, err
	}
	if len(original) > e.remote.config.MaxRequestBytes {
		return time.Time{}, ErrRegistrationProtocol
	}
	if p.Secrets.RegistrationRequest != "" && !bytes.Equal(original, []byte(p.Secrets.RegistrationRequest)) {
		return time.Time{}, ErrRegistrationProtocol
	}
	p.Secrets.RegistrationRequest = string(original)
	if !p.RemoteInFlight {
		p.RemoteInFlight = true
		p.InFlightAt = now
	}
	if err = e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil }); err != nil {
		return time.Time{}, err
	}
	result, callErr := e.remote.Register(ctx, p, original)
	// A cancelled network context does not discard a potentially committed call.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.config.WriteBudget)
	defer cancel()
	for range e.config.MaxCommitRetries {
		current, err := e.store.Load(saveCtx)
		if err != nil {
			return time.Time{}, err
		}
		latest, ok := current.Profiles[id]
		if !ok || latest.Attempt == nil || latest.Attempt.Binding != p.Attempt.Binding || latest.Secrets.RegistrationID != p.Secrets.RegistrationID || latest.Secrets.RegistrationRequest != p.Secrets.RegistrationRequest || latest.Attempt.AccountID != p.Attempt.AccountID || latest.Attempt.NetworkID != p.Attempt.NetworkID {
			return time.Time{}, state.ErrStale
		}
		if latest.Trust.KeyID != p.Trust.KeyID || latest.Trust.AnnouncementID != p.Trust.AnnouncementID || !bytes.Equal(latest.Trust.Bundle, p.Trust.Bundle) {
			return time.Time{}, state.ErrStale
		}
		next, err := e.apply(latest, result, callErr)
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
		if next.Attempt.Phase != enrollment.Registering || next.Registration.BlockedReason == "client.registration.invalid_response" {
			return time.Time{}, nil
		}
		return next.Registration.NextCallAt, nil
	}
	return time.Time{}, state.ErrStale
}

func (e *RegistrationExecutor) apply(p state.Profile, result RegisteredResult, callErr error) (state.Profile, error) {
	now := e.config.Now()
	a, err := p.Attempt.Observe(now)
	if err != nil {
		return p, err
	}
	if callErr != nil {
		if errors.Is(callErr, ErrBootstrap) {
			// Trust refusal happens before the mutation. Retain a previous unknown
			// call's marker rather than asserting that it rolled back remotely.
			if !a.Uncertain {
				p.RemoteInFlight = false
				p.InFlightAt = time.Time{}
			}
			p.Registration.BlockedReason = "client.registration.trust_required"
		} else {
			if a.Phase == enrollment.Registering {
				a, err = a.MarkUncertain(now)
			} else {
				a, err = a.RecoverLostCall(p.InFlightAt, now)
			}
			if err != nil {
				return p, err
			}
			if errors.Is(callErr, ErrRegistrationProtocol) {
				p.Registration.BlockedReason = "client.registration.invalid_response"
			}
		}
		p.Attempt = &a
		p.Registration.NextCallAt = registrationNextCall(a, now.Add(e.config.RetryInterval))
		return p, nil
	}
	if result.AccountID != a.AccountID || result.NetworkID != a.NetworkID || result.NodeID == "" || result.Credential == "" || !result.CredentialEnds.After(now) {
		return p, ErrRegistrationProtocol
	}
	// A fresh verified result is the only path to Registered. The projection
	// remains disconnected; no map or OS network effect is applied here.
	a, err = a.ResolveCommittedRegistration(result.NodeID, result.CredentialEnds, now)
	if err != nil {
		return p, err
	}
	p.Attempt = &a
	p.Secrets.NodeCredential = result.Credential
	p.RemoteInFlight = false
	p.InFlightAt = time.Time{}
	p.Registration = state.RegistrationProgress{}
	return p, nil
}

func registrationNextCall(a enrollment.Attempt, due time.Time) time.Time {
	for _, deadline := range []time.Time{a.Deadline, a.RecoveryUntil} {
		if !deadline.IsZero() && deadline.Before(due) {
			due = deadline
		}
	}
	return due
}
