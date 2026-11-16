package clientapp

import (
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

func TestCredentialScheduleAnchorsEarlyDeadlinesAcrossLifetimes(t *testing.T) {
	for _, ttl := range []time.Duration{time.Hour, 60 * time.Millisecond} {
		t.Run(ttl.String(), func(t *testing.T) {
			now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
			p := state.Profile{Attempt: &enrollment.Attempt{CredentialEnds: now.Add(ttl)}, Secrets: state.RemoteSecrets{SessionID: "first", SessionExpiresAt: now.Add(ttl), RenewalExpiresAt: now.Add(24 * time.Hour), NodeCredential: "initial"}}
			var schedule credentialSchedule
			plan, err := schedule.plan(p, now)
			if err != nil || plan.session || plan.node {
				t.Fatal("premature renewal", err)
			}
			want := now.Add(ttl - min(5*time.Minute, ttl/3))
			for _, at := range []time.Time{now, want.Add(-time.Nanosecond)} {
				plan, err = schedule.plan(p, at)
				if err != nil || !plan.next.Equal(want) || plan.session || plan.node {
					t.Fatal("poll shifted the renewal deadline", plan, err)
				}
			}
			plan, err = schedule.plan(p, want)
			if err != nil || !plan.session || !plan.node || !want.Before(p.Attempt.CredentialEnds) {
				t.Fatal("not due before expiry", plan, err)
			}
			// A newly admitted credential gets a new schedule, without a state migration.
			p.Secrets.SessionID = "rotated"
			p.Secrets.SessionExpiresAt = want.Add(ttl)
			p.Secrets.NodeCredential = "rotated-node"
			p.Attempt.CredentialEnds = want.Add(ttl)
			plan, err = schedule.plan(p, want)
			if err != nil || plan.session || plan.node || !plan.next.After(want) {
				t.Fatal("new credential renewed in a loop", plan, err)
			}
		})
	}
}

func TestCredentialSchedulePreservesFrozenNodeSessionBinding(t *testing.T) {
	now := time.Now()
	p := state.Profile{Attempt: &enrollment.Attempt{CredentialEnds: now.Add(time.Hour)}, Secrets: state.RemoteSecrets{SessionID: "first", SessionExpiresAt: now.Add(time.Minute), RenewalExpiresAt: now.Add(time.Hour)}, NodeRenewal: &state.NodeRenewal{Request: "frozen"}}
	var schedule credentialSchedule
	plan, err := schedule.plan(p, now.Add(50*time.Second))
	if err != nil || plan.session || !plan.node {
		t.Fatal("pending Node command did not take priority", plan, err)
	}
}
