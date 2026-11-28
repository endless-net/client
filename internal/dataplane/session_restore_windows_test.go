//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
)

func TestSessionRestoreUsesFreshAuthorityAndNewBoundWithoutRewritingIntent(t *testing.T) {
	s, store, _, old, ip, _, _, _, clock := wiredSessionFixtureAt(t, time.Now().UTC().Add(-2*time.Minute))
	if err := connectivity.PersistCursor(context.Background(), store, 3, old); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := before.Profiles[cleanupProfile].Connection
	if !original.Deadline.Before(time.Now()) {
		t.Fatal("fixture did not expire original intent deadline")
	}
	clock.advance(2 * time.Minute)
	fresh := nextSessionMap(t, s, old, clock, func(*coordinator.NetworkMapSnapshot) {})
	operation, _ := sessionFixtureOperation(t, s, cleanupContext(t), true)
	if err := s.Restore(operation, fresh); err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := after.Profiles[cleanupProfile].Connection
	if c.Generation != original.Generation || c.Intent != original.Intent || c.AcceptedAt != original.AcceptedAt || c.Deadline != original.Deadline || ip.createdAddresses != 1 || !s.lease.Permissions().NewFlows {
		t.Fatal("restore reset durable intent or reused expired budget")
	}
}

func TestSessionRestoreRetiresGrantThatExpiresDuringNativeApply(t *testing.T) {
	for _, stage := range []string{"route", "filter commit"} {
		t.Run(stage, func(t *testing.T) {
			s, store, _, old, ip, dynamic, _, _, clock := wiredSessionFixture(t)
			if err := connectivity.PersistCursor(t.Context(), store, s.generation, old); err != nil {
				t.Fatal(err)
			}
			clock.advance(time.Second)
			sample, _ := clock.Sample()
			end := sample.Wall.Add(2 * time.Second)
			fresh := nextSessionMap(t, s, old, clock, func(m *coordinator.NetworkMapSnapshot) {
				peer := &m.Peers[0]
				peer.ACLGrants[0].ValidUntil = &end
				peer.ACLGrants = append(peer.ACLGrants, coordinator.ACLGrant{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}})
			})
			plan, err := s.planFilters(t.Context(), fresh, s.id, 12345, 17, 16, 16, false)
			if err != nil {
				t.Fatal(err)
			}
			var timed, independent wf.RuleID
			for _, r := range plan.rules {
				if r.Layer == wf.LayerALEAuthConnectV4 && r.Action == wf.ActionPermit {
					if plan.grantEnds[r.ID].IsZero() {
						independent = r.ID
					} else {
						timed = r.ID
					}
				}
			}
			if timed.IsZero() || independent.IsZero() {
				t.Fatal("missing fixture grant")
			}
			advanced := false
			if stage == "route" {
				check := ip.afterCreate
				ip.afterCreate = func() {
					check()
					if !advanced {
						clock.advance(2 * time.Second)
						advanced = true
					}
				}
			}
			dynamic.onCommit = func() {
				if dynamic.live[timed] != 0 {
					if advanced {
						t.Fatal("expired permit installed after cutoff")
					}
					clock.advance(2 * time.Second)
					advanced = true
				}
			}
			operation, _ := sessionFixtureOperation(t, s, cleanupContext(t), true)
			if err := s.Restore(operation, fresh); err != nil {
				t.Fatal(err)
			}
			if !advanced || dynamic.live[timed] != 0 || dynamic.live[independent] == 0 || !s.policy.nextGrantEnd().IsZero() || !s.lease.Permissions().NewFlows {
				t.Fatal("restore returned with incorrect permissions")
			}
		})
	}
}

func TestSessionRestoreRejectsCachedMapUnboundedContextAndOldGeneration(t *testing.T) {
	for _, failure := range []string{"cached", "unbounded", "long-bound", "old-generation"} {
		s, store, _, old, ip, _, _, tunnel, clock := wiredSessionFixture(t)
		m := old
		if failure != "cached" {
			clock.advance(time.Second)
			m = nextSessionMap(t, s, old, clock, func(*coordinator.NetworkMapSnapshot) {})
		}
		ctx := cleanupContext(t)
		if failure == "unbounded" {
			ctx = context.Background()
		}
		if failure == "long-bound" {
			long, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			ctx = long
		}
		expected := ErrConfiguration
		if failure == "old-generation" {
			value, err := store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Update(ctx, value.Revision, func(next *state.State) error {
				p := next.Profiles[cleanupProfile]
				_, err := p.Connection.BeginRequestedAttempt(state.IntentConnected, p.Connection.AcceptedAt.Add(time.Second), state.ConnectionRequest{InstanceID: "5e2c63ba-c810-4b04-af73-4c64ef0f24ab", Revision: value.Revision})
				next.Profiles[cleanupProfile] = p
				return err
			}); err != nil {
				t.Fatal(err)
			}
			expected = state.ErrStale
		}
		operation, _ := sessionFixtureOperation(t, s, ctx, true)
		if err := s.Restore(operation, m); !errors.Is(err, expected) || ip.createdAddresses+ip.createdRoutes != 0 || tunnel.closes != 0 || s.lease.Permissions().ExistingFlows {
			t.Fatal("restore accepted cached authority or unsafe bound", failure, err)
		}
	}
}
