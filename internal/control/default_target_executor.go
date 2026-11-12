package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type DefaultTargetExecutor struct {
	run    sync.Mutex
	store  *state.Store
	remote *DefaultTarget
	config RegistrationExecutorConfig
}

func NewDefaultTargetExecutor(store *state.Store, remote *DefaultTarget, config RegistrationExecutorConfig) (*DefaultTargetExecutor, error) {
	if store == nil || remote == nil || config.MaxCommitRetries <= 0 || config.RetryInterval <= 0 || config.RetryInterval > 30*time.Second || config.WriteBudget <= 0 || config.WriteBudget > 30*time.Second {
		return nil, ErrDefaultTargetProtocol
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &DefaultTargetExecutor{store: store, remote: remote, config: config}, nil
}

// Reconcile persists the original signed request/send marker before one call.
// No retry creates another resolver identity, device key or session.
func (e *DefaultTargetExecutor) Reconcile(ctx context.Context, id, attempt string) (time.Time, error) {
	if !e.run.TryLock() {
		return time.Time{}, state.ErrStale
	}
	defer e.run.Unlock()
	s, err := e.store.Load(ctx)
	if err != nil {
		return time.Time{}, err
	}
	p, ok := s.Profiles[id]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt || p.Attempt.Phase != enrollment.ChoosingContext {
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
	if a.Phase != enrollment.ChoosingContext || p.Trust.KeyID == "" || p.DefaultTarget != nil && p.DefaultTarget.BlockedReason == "client.default_target.invalid_response" {
		return time.Time{}, nil
	}
	if p.DefaultTarget != nil && now.Before(p.DefaultTarget.NextCallAt) {
		return p.DefaultTarget.NextCallAt, nil
	}
	original, err := PrepareDefaultTarget(p)
	if err != nil {
		return time.Time{}, err
	}
	if p.DefaultTarget != nil && p.DefaultTarget.Request != string(original) {
		return time.Time{}, ErrDefaultTargetProtocol
	}
	if p.DefaultTarget == nil {
		p.DefaultTarget = &state.DefaultTargetProgress{Request: string(original)}
	}
	if !p.RemoteInFlight {
		p.RemoteInFlight = true
		p.InFlightAt = now
	}
	if err = e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil }); err != nil {
		return time.Time{}, err
	}
	result, callErr := e.remote.Resolve(ctx, p, original)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.config.WriteBudget)
	defer cancel()
	for range e.config.MaxCommitRetries {
		current, err := e.store.Load(saveCtx)
		if err != nil {
			return time.Time{}, err
		}
		latest, ok := current.Profiles[id]
		if !ok || latest.Attempt == nil || latest.Attempt.Binding != p.Attempt.Binding || latest.DefaultTarget == nil || latest.DefaultTarget.Request != string(original) || latest.Secrets.Access != p.Secrets.Access {
			return time.Time{}, state.ErrStale
		}
		if latest.Attempt.Phase == enrollment.Registering || latest.Attempt.Phase == enrollment.Registered {
			return time.Time{}, nil
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
		if next.Attempt.Phase == enrollment.Registering {
			return e.config.Now(), nil
		}
		if next.Attempt.Phase != enrollment.ChoosingContext || next.DefaultTarget.BlockedReason == "client.default_target.invalid_response" {
			return time.Time{}, nil
		}
		return next.DefaultTarget.NextCallAt, nil
	}
	return time.Time{}, state.ErrStale
}

func (e *DefaultTargetExecutor) apply(p state.Profile, result coordinator.DefaultEnrollmentTarget, callErr error) (state.Profile, error) {
	now := e.config.Now()
	a, err := p.Attempt.Observe(now)
	if err != nil {
		return p, err
	}
	progress := *p.DefaultTarget
	p.DefaultTarget = &progress
	if callErr != nil {
		if errors.Is(callErr, ErrBootstrap) {
			if !a.Uncertain {
				p.RemoteInFlight = false
				p.InFlightAt = time.Time{}
			}
			progress.BlockedReason = "client.default_target.trust_required"
		} else {
			if a.Phase == enrollment.ChoosingContext {
				a, err = a.MarkUncertain(now)
			} else {
				a, err = a.RecoverLostCall(p.InFlightAt, now)
			}
			if err != nil {
				return p, err
			}
			if errors.Is(callErr, ErrDefaultTargetProtocol) {
				progress.BlockedReason = "client.default_target.invalid_response"
			}
		}
		p.Attempt = &a
		progress.NextCallAt = now.Add(e.config.RetryInterval)
		if a.Deadline.Before(progress.NextCallAt) {
			progress.NextCallAt = a.Deadline
		}
		if a.Uncertain && a.RecoveryUntil.Before(progress.NextCallAt) {
			progress.NextCallAt = a.RecoveryUntil
		}
		return p, nil
	}
	// Cancellation/expiry wins locally: a resolved target is never a grant and
	// cannot restart a cancelled registration or create a fresh attempt.
	if a.Phase != enrollment.ChoosingContext {
		p.Attempt = &a
		p.RemoteInFlight = false
		p.InFlightAt = time.Time{}
		return p, nil
	}
	r, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest([]byte(progress.Request), coordinator.DefaultTargetMaxBytes)
	if err != nil || result.Match(r, now) != nil {
		return p, ErrDefaultTargetProtocol
	}
	a, err = a.Select(result.AccountID, result.NetworkID, now)
	if err != nil {
		return p, err
	}
	// A known resolver outcome resolves only this call's uncertainty.
	a.Uncertain = false
	raw, err := json.Marshal(result)
	if err != nil {
		return p, ErrDefaultTargetProtocol
	}
	progress.Receipt = string(raw)
	progress.BlockedReason = ""
	progress.NextCallAt = time.Time{}
	p.Attempt = &a
	p.Secrets.RegistrationID = r.RequestID
	p.RemoteInFlight = false
	p.InFlightAt = time.Time{}
	return p, nil
}
