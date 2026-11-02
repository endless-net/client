package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

const connectionRuntime = "5e2c63ba-c810-4b04-af73-4c64ef0f24ab"

func beginConnectionRequest(s *Store, intent DesiredIntent, now time.Time) error {
	value, err := s.Load(context.Background())
	if err != nil {
		return err
	}
	return s.Update(context.Background(), value.Revision, func(next *State) error {
		p := next.Profiles[profileID]
		if p.Connection == nil {
			p.Connection = &ConnectionState{}
		}
		_, err := p.Connection.BeginRequestedAttempt(intent, now, ConnectionRequest{InstanceID: connectionRuntime, Revision: value.Revision})
		next.Profiles[profileID] = p
		return err
	})
}

func TestRequestedRetryHasNewGenerationButExactReplayCannotRenewBudget(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	b.failAfter = true
	if err := beginConnectionRequest(s, IntentConnected, now); !errors.Is(err, ErrUncertain) {
		t.Fatal("lost command ACK claimed success", err)
	}
	b.failAfter = false
	restarted, err := New(b, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	c := storedConnection(t, restarted)
	original := *c.Request
	if c.Generation != 1 || c.Deadline != now.Add(30*time.Second) {
		t.Fatal("command identity/deadline lost")
	}
	if changed, err := c.BeginRequestedAttempt(IntentConnected, now.Add(time.Hour), original); err != nil || changed || c.Generation != 1 || c.Deadline != now.Add(30*time.Second) {
		t.Fatal("exact command replay extended deadline", err)
	}
	if err := connectionChange(s, func(c *ConnectionState) {
		c.Journal = &ResourceJournal{ID: journalID, Generation: c.Generation, Adapter: &AdapterRecord{ID: journalID, Name: "old cleanup", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := beginConnectionRequest(s, IntentConnected, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c = storedConnection(t, s)
	if c.Generation != 2 || c.Intent != IntentConnected || c.Deadline != now.Add(time.Hour+30*time.Second) || c.Journal == nil || c.Journal.Generation != 1 || c.Request.Revision <= original.Revision {
		t.Fatal("new request reset cleanup or did not fence old attempt")
	}
	if err := beginConnectionRequest(s, IntentDisconnected, now.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	c = storedConnection(t, s)
	if c.Generation != 3 || c.Intent != IntentDisconnected || c.Deadline != now.Add(time.Hour+11*time.Second) || c.Journal.Generation != 1 {
		t.Fatal("disconnect command lost original identity/cleanup")
	}
}

func TestRequestedAttemptCannotForgeCASKeyDropIdentityOrRewriteDeadline(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	if err := beginConnectionRequest(s, IntentConnected, now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Request = nil },
		func(c *ConnectionState) { c.Request.Revision++ },
		func(c *ConnectionState) { c.Request.InstanceID = journalID },
		func(c *ConnectionState) {
			c.AcceptedAt = c.AcceptedAt.Add(time.Second)
			c.Deadline = c.Deadline.Add(time.Second)
		},
		func(c *ConnectionState) {
			c.Intent = IntentDisconnected
			c.Generation++
			c.AcceptedAt = c.AcceptedAt.Add(time.Second)
			c.Deadline = c.AcceptedAt.Add(10 * time.Second)
		},
	} {
		if err := connectionChange(s, change); !errors.Is(err, ErrCorrupt) {
			t.Fatal("command identity was rebound", err)
		}
	}
	value, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(context.Background(), value.Revision, func(next *State) error {
		p := next.Profiles[profileID]
		_, err := p.Connection.BeginRequestedAttempt(IntentConnected, now.Add(time.Minute), ConnectionRequest{InstanceID: connectionRuntime, Revision: value.Revision + 10})
		next.Profiles[profileID] = p
		return err
	})
	if !errors.Is(err, ErrCorrupt) {
		t.Fatal("request did not consume exact CAS revision", err)
	}
	if _, err := storedConnection(t, s).BeginRequestedAttempt(IntentDisconnected, now.Add(time.Minute), ConnectionRequest{InstanceID: connectionRuntime, Revision: 1}); !errors.Is(err, ErrStale) {
		t.Fatal("old command accepted", err)
	}
}

func TestConnectionCounterReceiptSurvivesACKLossAndCannotBeRebased(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	value, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b.failAfter = true
	err = s.Update(context.Background(), value.Revision, func(next *State) error {
		p := next.Profiles[profileID]
		p.Connection = &ConnectionState{}
		_, err := p.Connection.BeginRequestedAttempt(IntentConnected, now, ConnectionRequest{InstanceID: connectionRuntime, Revision: value.Revision, ClockEpoch: connectionRuntime, Uptime: time.Hour, UpperUptime: time.Hour + time.Millisecond})
		next.Profiles[profileID] = p
		return err
	})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal("lost receipt ACK claimed success", err)
	}
	b.failAfter = false
	c := storedConnection(t, s)
	if c.Request.ClockEpoch != connectionRuntime || c.Request.Uptime != time.Hour || c.Request.UpperUptime != time.Hour+time.Millisecond {
		t.Fatal("receipt changed after durable ACK loss")
	}
	for _, change := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Request.ClockEpoch = ""; c.Request.Uptime = 0; c.Request.UpperUptime = 0 },
		func(c *ConnectionState) { c.Request.Uptime += time.Second; c.Request.UpperUptime += time.Second },
		func(c *ConnectionState) { c.Request.ClockEpoch = journalID },
	} {
		if err := connectionChange(s, change); !errors.Is(err, ErrCorrupt) {
			t.Fatal("accepted counter receipt rebound", err)
		}
	}
}
