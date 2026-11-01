package control

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type ApprovalExecutorConfig struct {
	MaxCommitRetries           int
	RetryInterval, WriteBudget time.Duration
	Now                        func() time.Time
}

type ApprovalExecutor struct {
	run    sync.Mutex
	store  *state.Store
	remote *Approval
	config ApprovalExecutorConfig
}

func NewApprovalExecutor(store *state.Store, remote *Approval, c ApprovalExecutorConfig) (*ApprovalExecutor, error) {
	if store == nil || remote == nil || c.Now == nil || c.MaxCommitRetries <= 0 || c.RetryInterval < time.Millisecond || c.RetryInterval > 30*time.Second || c.WriteBudget < time.Millisecond || c.WriteBudget > 30*time.Second {
		return nil, approvalmodel.ErrProtocol
	}
	return &ApprovalExecutor{store: store, remote: remote, config: c}, nil
}

func approvalActive(p state.Profile) bool {
	return p.Attempt != nil && p.Attempt.BrowserApproval && p.Approval != nil && (p.Attempt.Phase == enrollment.PendingApproval || p.Attempt.Phase == enrollment.Completing) && p.Approval.BlockedReason != "client.approval.invalid_response"
}

func approvalDue(p state.Profile, due time.Time) time.Time {
	if !approvalActive(p) || due.IsZero() {
		return time.Time{}
	}
	a := p.Attempt
	ends := []time.Time{a.Deadline}
	if a.Uncertain {
		ends = append(ends, a.RecoveryUntil)
	} else {
		ends = append(ends, a.ApprovalUntil)
		if p.Approval.Snapshot != nil {
			ends = append(ends, p.Approval.Snapshot.Request.ExpiresAt)
		}
	}
	for _, end := range ends {
		if !end.IsZero() && end.Before(due) {
			due = end
		}
	}
	return due
}

// Reconcile performs at most one bounded original remote call. The caller must
// have explicitly persisted PrepareApprovalState; no target or ID is invented.
func (e *ApprovalExecutor) Reconcile(ctx context.Context, id, attempt string) (time.Time, error) {
	if !e.run.TryLock() {
		return time.Time{}, state.ErrStale
	}
	defer e.run.Unlock()
	s, err := e.store.Load(ctx)
	if err != nil {
		return time.Time{}, err
	}
	p, ok := s.Profiles[id]
	if !ok || p.Attempt == nil || p.Attempt.AttemptID != attempt || !p.Attempt.BrowserApproval || p.Approval == nil {
		return time.Time{}, nil
	}
	if p.Approval.Base != e.remote.base {
		return time.Time{}, approvalmodel.ErrProtocol
	}
	progress := *p.Approval
	p.Approval = &progress
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
	p.Approval.NextCallAt = approvalDue(p, p.Approval.NextCallAt)
	if a != *s.Profiles[id].Attempt || p.Approval.NextCallAt != s.Profiles[id].Approval.NextCallAt {
		if err := e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil }); err != nil {
			return time.Time{}, err
		}
		s.Revision++
	}
	if !approvalActive(p) || p.Trust.KeyID == "" {
		return time.Time{}, nil
	}
	if now.Before(p.Approval.NextCallAt) {
		return p.Approval.NextCallAt, nil
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if !p.RemoteInFlight {
		p.RemoteInFlight = true
		p.InFlightAt = now
	}
	if err := e.store.Update(ctx, s.Revision, func(s *state.State) error { s.Profiles[id] = p; return nil }); err != nil {
		return time.Time{}, err
	}
	result, callErr := e.call(ctx, p)
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), e.config.WriteBudget)
	defer stop()
	for range e.config.MaxCommitRetries {
		current, err := e.store.Load(save)
		if err != nil {
			return time.Time{}, err
		}
		latest, ok := current.Profiles[id]
		if !ok || latest.Attempt == nil || latest.Attempt.Binding != p.Attempt.Binding || !latest.Attempt.BrowserApproval || latest.Approval == nil || latest.Approval.Base != p.Approval.Base || latest.Secrets.RegistrationID != p.Secrets.RegistrationID || latest.Secrets.RegistrationRequest != p.Secrets.RegistrationRequest || latest.Trust.KeyID != p.Trust.KeyID || latest.Trust.AnnouncementID != p.Trust.AnnouncementID || !bytes.Equal(latest.Trust.Bundle, p.Trust.Bundle) {
			return time.Time{}, state.ErrStale
		}
		next, err := e.apply(latest, result, callErr)
		if err != nil {
			return time.Time{}, err
		}
		err = e.store.Update(save, current.Revision, func(s *state.State) error { s.Profiles[id] = next; return nil })
		if errors.Is(err, state.ErrStale) {
			continue
		}
		if err != nil {
			return time.Time{}, err
		}
		return next.Approval.NextCallAt, nil
	}
	return time.Time{}, state.ErrStale
}

func (e *ApprovalExecutor) call(ctx context.Context, p state.Profile) (ApprovalCompletion, error) {
	original := []byte(p.Secrets.RegistrationRequest)
	previous := p.Approval.Snapshot
	if previous == nil {
		s, err := e.remote.Create(ctx, p, original, nil)
		return ApprovalCompletion{Snapshot: s}, err
	}
	if previous.Request.Status == "approved" || previous.Request.Status == "enrolled" {
		return e.remote.Complete(ctx, p, original, *previous)
	}
	s, err := e.remote.Poll(ctx, p, original, *previous)
	return ApprovalCompletion{Snapshot: s}, err
}

func (e *ApprovalExecutor) apply(p state.Profile, result ApprovalCompletion, callErr error) (state.Profile, error) {
	now := e.config.Now()
	var a enrollment.Attempt
	var err error
	if result.Registered != nil && p.RemoteInFlight && !now.Before(p.Attempt.ApprovalUntil) {
		a, err = p.Attempt.RecoverLostCall(p.InFlightAt, now)
	} else {
		a, err = p.Attempt.Observe(now)
	}
	if err != nil {
		return p, err
	}
	progress := *p.Approval
	p.Approval = &progress
	if callErr != nil {
		if errors.Is(callErr, ErrBootstrap) || errors.Is(callErr, ErrApprovalNotDue) {
			if !a.Uncertain {
				p.RemoteInFlight = false
				p.InFlightAt = time.Time{}
			}
			if errors.Is(callErr, ErrBootstrap) {
				p.Approval.BlockedReason = "client.approval.trust_required"
			}
		} else {
			a, err = p.Attempt.RecoverLostCall(p.InFlightAt, now)
			if err != nil {
				return p, err
			}
			if errors.Is(callErr, approvalmodel.ErrProtocol) || errors.Is(callErr, ErrRegistrationProtocol) {
				p.Approval.BlockedReason = "client.approval.invalid_response"
			}
		}
		p.Attempt = &a
		p.Approval.NextCallAt = e.next(p, now)
		return p, nil
	}
	snapshot := result.Snapshot
	if snapshot.ObservedAt.After(now) || snapshot.ObservedAt.Before(p.Attempt.ObservedAt) || p.Approval.Snapshot != nil && (snapshot.PollToken != p.Approval.Snapshot.PollToken || !approvalSuccessor(p.Approval.Snapshot.Request, snapshot.Request)) {
		return p, state.ErrStale
	}
	if a.Phase == enrollment.Expired {
		// Acceptance missed the original local budget. Retain uncertainty about
		// the already-dispatched operation; do not install a late credential.
		a, err = a.RecoverLostCall(p.InFlightAt, now)
		if err != nil {
			return p, err
		}
		p.Attempt = &a
		p.Approval.NextCallAt = time.Time{}
		return p, nil
	}
	// A concurrent local cancel retains its phase, but may save original remote
	// facts returned by the in-flight call. Snapshot time never moves backwards.
	if now.After(a.ObservedAt) {
		a.ObservedAt = now
		a.Revision++
	}
	p.Approval.Snapshot = &snapshot
	p.Approval.BlockedReason = ""
	status := snapshot.Request.Status
	if status == "approved" || status == "enrolled" {
		a, err = a.BindBrowserContext(snapshot.Request.AccountID, snapshot.Request.NetworkID, now)
		if err != nil {
			return p, err
		}
		if result.Registered != nil {
			r := result.Registered
			if status != "enrolled" || r.AccountID != a.AccountID || r.NetworkID != a.NetworkID || r.NodeID != snapshot.Request.NodeID || r.Credential == "" || !r.CredentialEnds.After(now) {
				return p, approvalmodel.ErrProtocol
			}
			public, err := e.remote.registration.credentialKey(p, r.Credential, now)
			if err != nil {
				return p, err
			}
			claims, err := coordinator.VerifyNodeCredential(r.Credential, e.remote.registration.config.MaxCredentialBytes, public, r.NetworkID, r.NodeID, "node:register", now)
			if err != nil || !claims.ExpiresAt.Equal(r.CredentialEnds) {
				return p, approvalmodel.ErrProtocol
			}
			a, err = a.ResolveCommittedRegistration(r.NodeID, r.CredentialEnds, now)
			if err != nil {
				return p, err
			}
			p.Secrets.NodeCredential = r.Credential
		} else if a.Phase == enrollment.PendingApproval {
			a, err = a.Approved(now)
		}
	} else if result.Registered != nil {
		return p, approvalmodel.ErrProtocol
	} else if status == "rejected" {
		a, err = a.Reject(now)
		if err == nil {
			a.Uncertain = false
		}
	} else {
		a.Uncertain = false
		a, err = a.Observe(now)
	}
	if err != nil {
		return p, err
	}
	p.Attempt = &a
	p.RemoteInFlight = false
	p.InFlightAt = time.Time{}
	p.Approval.NextCallAt = e.next(p, now)
	return p, nil
}

func (e *ApprovalExecutor) next(p state.Profile, now time.Time) time.Time {
	due := now.Add(e.config.RetryInterval)
	if s := p.Approval.Snapshot; s != nil && s.Request.Status == "pending" {
		hint := s.ObservedAt.Add(time.Duration(s.PollAfterSeconds) * time.Second)
		if hint.After(due) {
			due = hint
		}
	}
	return approvalDue(p, due)
}
