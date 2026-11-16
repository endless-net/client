package clientapp

import (
	"time"

	"github.com/endless-net/client/internal/state"
)

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

func (s *credentialSchedule) plan(p state.Profile, now time.Time) (credentialPlan, error) {
	current, err := p.CurrentSession()
	if err != nil {
		return credentialPlan{}, err
	}
	credential, expires, err := p.CurrentNodeCredential()
	if err != nil {
		return credentialPlan{}, err
	}
	sessionExpiry := current.ExpiresAt
	if current.RenewalExpiresAt.Before(sessionExpiry) {
		sessionExpiry = current.RenewalExpiresAt
	}
	sessionAt := s.session.observe(current.SessionID, sessionExpiry, now)
	nodeAt := s.node.observe(state.CredentialDigest(credential), expires, now)
	plan := credentialPlan{session: !now.Before(sessionAt), node: !now.Before(nodeAt), next: sessionAt}
	if nodeAt.Before(plan.next) {
		plan.next = nodeAt
	}
	if p.SessionRefresh != nil && (p.ActiveSession == nil || p.ActiveSession.RequestID != p.SessionRefresh.RequestID) {
		plan.session = true
	}
	if p.NodeRenewal != nil && (p.ActiveNodeCredential == nil || p.ActiveNodeCredential.Request != p.NodeRenewal.Request) {
		// Recover the frozen session-bound Node command before rotating its bearer.
		plan.session, plan.node = false, true
	}
	return plan, nil
}
