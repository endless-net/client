package clientapp

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
)

func credentialRetry(err error) bool {
	return errors.Is(err, control.ErrCredentialsUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, state.ErrUncertain) || errors.Is(err, state.ErrStale)
}

// A deadline is anchored once per accepted credential, not moved closer to
// expiry on every poll. Restart observes the remaining lifetime afresh.
type credentialDeadline struct {
	key         string
	expires, at time.Time
}

func (d *credentialDeadline) observe(key string, expires, now time.Time) time.Time {
	if d.key != key || !d.expires.Equal(expires) || d.at.IsZero() {
		d.key, d.expires = key, expires
		d.at = expires.Add(-min(5*time.Minute, max(0, expires.Sub(now)/3)))
	}
	return d.at
}

type credentialSchedule struct{ session, node credentialDeadline }

type credentialPlan struct {
	session, node bool
	next          time.Time
}

func (d *credentialDeadline) observeSession(p state.Profile, now time.Time) (at time.Time, due bool, err error) {
	current, err := p.CurrentSession()
	if err != nil {
		return time.Time{}, false, err
	}
	expires := current.ExpiresAt
	if current.RenewalExpiresAt.Before(expires) {
		expires = current.RenewalExpiresAt
	}
	at = d.observe(current.SessionID, expires, now)
	pending := p.SessionRefresh != nil && (p.ActiveSession == nil || p.ActiveSession.RequestID != p.SessionRefresh.RequestID)
	return at, pending || !now.Before(at), nil
}

func (s *credentialSchedule) plan(p state.Profile, now time.Time) (credentialPlan, error) {
	sessionAt, sessionDue, err := s.session.observeSession(p, now)
	if err != nil {
		return credentialPlan{}, err
	}
	credential, expires, err := p.CurrentNodeCredential()
	if err != nil {
		return credentialPlan{}, err
	}
	nodeAt := s.node.observe(state.CredentialDigest(credential), expires, now)
	plan := credentialPlan{session: sessionDue, node: !now.Before(nodeAt), next: sessionAt}
	if nodeAt.Before(plan.next) {
		plan.next = nodeAt
	}
	if p.NodeRenewal != nil && (p.ActiveNodeCredential == nil || p.ActiveNodeCredential.Request != p.NodeRenewal.Request) {
		// Recover the frozen session-bound Node command before rotating its bearer.
		plan.session, plan.node = false, true
	}
	return plan, nil
}
