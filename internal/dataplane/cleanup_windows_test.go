//go:build windows

package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wf"
)

type cleanupMemoryStore struct {
	value             state.State
	writes, loseACKAt int
	stale             bool
}

func cloneCleanupState(value state.State) state.State {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var result state.State
	if json.Unmarshal(raw, &result) != nil {
		panic("invalid fixture state")
	}
	return result
}

func (s *cleanupMemoryStore) Load(ctx context.Context) (state.State, error) {
	if err := ctx.Err(); err != nil {
		return state.State{}, err
	}
	return cloneCleanupState(s.value), nil
}
func (s *cleanupMemoryStore) Update(ctx context.Context, revision uint64, change func(*state.State) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.stale {
		s.value.Revision++
		s.stale = false
	}
	if s.value.Revision != revision {
		return state.ErrStale
	}
	next := cloneCleanupState(s.value)
	if err := change(&next); err != nil {
		return err
	}
	next.Revision++
	s.value = next
	s.writes++
	if s.writes == s.loseACKAt {
		return state.ErrUncertain
	}
	return nil
}

type cleanupAdapterFunc func(context.Context, state.AdapterRecord) error

func (f cleanupAdapterFunc) ConfirmAbsent(ctx context.Context, record state.AdapterRecord) error {
	return f(ctx, record)
}

const cleanupProfile = "862e4ef0-fe90-4b89-bbbd-6e5f67bf8c56"

func cleanupFixture(t *testing.T) (*WindowsCleanup, *cleanupMemoryStore, *mockIPAPI, *mockFilterAPI) {
	t.Helper()
	_, ipAPI, _, journal := ipFixture(t)
	ipAPI.gone = true // original adapter/rows disappeared with the old process
	p := policyFixture(t, false)
	journal.Filters = p.Records()
	for i := range journal.Filters {
		journal.Filters[i].ID = uint64(100 + i)
	}
	store := &cleanupMemoryStore{value: state.State{Revision: 1, ActiveProfileID: cleanupProfile, Profiles: map[string]state.Profile{
		cleanupProfile: {ID: cleanupProfile, Secrets: state.RemoteSecrets{NodeCredential: "credential fixture"}, Connection: &state.ConnectionState{Intent: state.IntentConnected, Generation: 3, Journal: &journal}},
	}}}
	guardAPI := &mockFilterAPI{persistent: true, layerLive: true, live: make(map[wf.RuleID]uint64)}
	for i, r := range p.rules {
		if r.Persistent {
			guardAPI.live[r.ID] = journal.Filters[i].ID
		}
	}
	c := &WindowsCleanup{store: store, ip: &WindowsIP{api: ipAPI, maxRows: 128}, adapter: cleanupAdapterFunc(func(context.Context, state.AdapterRecord) error { return nil })}
	c.guards = func(_ context.Context, j state.ResourceJournal) (recoveredGuards, error) {
		if len(j.Addresses)+len(j.Routes) != 0 || j.Adapter.Phase != state.EffectRemoving {
			t.Fatal("guards retired before IP journal acknowledgments")
		}
		plan, ids, err := recoverGuardPlan(j, 8)
		if err != nil {
			return nil, err
		}
		g := &persistentGuards{api: guardAPI, key: plan.session, rules: make(map[wf.RuleID]*wf.Rule), ids: ids, started: true}
		for _, r := range plan.rules {
			g.rules[r.ID] = r
		}
		return &WindowsFilters{session: plan.session, guards: g, attempted: &plan, recoveryOnly: true, dynamicClosed: true}, nil
	}
	return c, store, ipAPI, guardAPI
}

func cleanupContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestWindowsCleanupComposesNativeAbsenceAndPreservesNewerIntent(t *testing.T) {
	c, store, ip, guards := cleanupFixture(t)
	ctx := cleanupContext(t)
	if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil {
		t.Fatal(err)
	}
	p := store.value.Profiles[cleanupProfile]
	if p.Connection.Journal != nil || p.Connection.Generation != 3 || p.Connection.Intent != state.IntentConnected || p.Secrets.NodeCredential != "credential fixture" {
		t.Fatal("cleanup changed current intent/registration or retained resolved effects")
	}
	if len(guards.live) != 0 || guards.layerLive || ip.createdRoutes+ip.createdAddresses+ip.deletedRoutes+ip.deletedAddresses != 0 {
		t.Fatal("cleanup recreated resources or mutated absent IP rows")
	}
	writes := store.writes
	if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil || store.writes != writes {
		t.Fatal("retired journal not idempotent", err)
	}
}

func TestWindowsCleanupLostStateACKCanResumeWithoutRestoringAuthority(t *testing.T) {
	for lostAt := 1; lostAt <= 6; lostAt++ {
		c, store, ip, guards := cleanupFixture(t)
		store.loseACKAt = lostAt
		ctx := cleanupContext(t)
		if err := c.Run(ctx, cleanupProfile, policySession, 1); !errors.Is(err, state.ErrUncertain) {
			t.Fatal("lost state ACK reported success", lostAt, err)
		}
		store.loseACKAt = 0
		if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil {
			t.Fatal("explicit recovery could not finish", lostAt, err)
		}
		if store.value.Profiles[cleanupProfile].Connection.Journal != nil || len(guards.live) != 0 || ip.createdRoutes+ip.createdAddresses != 0 {
			t.Fatal("recovery retained/recreated authority")
		}
	}
}

func TestWindowsCleanupLiveForeignOrStaleStateStopsBeforeDeletion(t *testing.T) {
	for _, failure := range []error{ErrAdapterCleanupRequired, ErrAdapterOwnership, ErrAdapterUncertain} {
		c, store, ip, guards := cleanupFixture(t)
		c.adapter = cleanupAdapterFunc(func(context.Context, state.AdapterRecord) error { return failure })
		if err := c.Run(cleanupContext(t), cleanupProfile, policySession, 1); !errors.Is(err, failure) || store.writes != 0 || guards.removes != 0 || ip.deletedRoutes+ip.deletedAddresses != 0 {
			t.Fatal("live/foreign adapter bypassed barrier", err)
		}
	}
	c, store, _, guards := cleanupFixture(t)
	store.stale = true
	if err := c.Run(cleanupContext(t), cleanupProfile, policySession, 1); !errors.Is(err, state.ErrStale) || store.writes != 0 || guards.removes != 0 {
		t.Fatal("CAS failure retried/mutated native state", err)
	}
	if err := c.Run(context.Background(), cleanupProfile, policySession, 1); err != ErrConfiguration {
		t.Fatal("unbounded cleanup allowed", err)
	}
}

func TestWindowsCleanupLostBFECommitRetainsJournalUntilExplicitRecovery(t *testing.T) {
	c, store, _, guards := cleanupFixture(t)
	guards.fail = "commit"
	ctx := cleanupContext(t)
	if err := c.Run(ctx, cleanupProfile, policySession, 1); !errors.Is(err, ErrPolicyUncertain) {
		t.Fatal("lost BFE commit called success", err)
	}
	j := store.value.Profiles[cleanupProfile].Connection.Journal
	if j == nil || len(j.Filters) == 0 || j.Adapter == nil || j.Adapter.Phase != state.EffectRemoving || guards.closes != 1 {
		t.Fatal("uncertain BFE cleanup erased journal or leaked recovery handle")
	}
	guards.fail = ""
	if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil || store.value.Profiles[cleanupProfile].Connection.Journal != nil {
		t.Fatal("absent committed guards could not retire", err)
	}
}

func TestWindowsCleanupRetainsGuardWhenUnjournaledNativeRowsRemain(t *testing.T) {
	c, store, ip, guards := cleanupFixture(t)
	ip.forward = append(ip.forward, testIPRoute(12345, "100.64.0.99/32", 7))
	if err := c.Run(cleanupContext(t), cleanupProfile, policySession, 1); !errors.Is(err, ErrPolicyCleanupRequired) || guards.removes != 0 || store.value.Profiles[cleanupProfile].Connection.Journal == nil {
		t.Fatal("leftover native row bypassed guard barrier", err)
	}
}

func TestWindowsCleanupUnjournaledWFPChildAbortsOwnedDeletes(t *testing.T) {
	c, store, _, guards := cleanupFixture(t)
	key, err := canonicalGUID("c6bb5e3f-7b9f-42f6-a542-d7bb3bd14427")
	if err != nil {
		t.Fatal(err)
	}
	guards.live[wf.RuleID(key)] = 9999
	before := len(guards.live)
	if err := c.Run(cleanupContext(t), cleanupProfile, policySession, 1); !errors.Is(err, ErrPolicy) || len(guards.live) != before || store.value.Profiles[cleanupProfile].Connection.Journal == nil {
		t.Fatal("unknown sublayer child erased owned guards/journal", err)
	}
}

func TestWindowsCleanupCancellationAfterBFECommitKeepsUnacknowledgedJournal(t *testing.T) {
	c, store, _, guards := cleanupFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	guards.onCommit = cancel
	if err := c.Run(ctx, cleanupProfile, policySession, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled cleanup claimed all acknowledgments", err)
	}
	j := store.value.Profiles[cleanupProfile].Connection.Journal
	if j == nil || len(j.Filters) == 0 || len(guards.live) != 0 {
		t.Fatal("unacknowledged native result lost")
	}
	guards.onCommit = nil
	if err := c.Run(cleanupContext(t), cleanupProfile, policySession, 1); err != nil {
		t.Fatal(err)
	}
}
