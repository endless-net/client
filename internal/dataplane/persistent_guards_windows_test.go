//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wf"
)

func guardRecoveryFixture(t *testing.T, records []state.FilterRecord) (FilterPlan, map[wf.RuleID]uint64) {
	t.Helper()
	j := state.ResourceJournal{ID: policySession, Adapter: &state.AdapterRecord{ID: policySession, LUID: 12345}, Filters: records}
	p, ids, err := recoverGuardPlan(j, 8)
	if err != nil {
		t.Fatal(err)
	}
	return p, ids
}

func TestPersistentGuardsWriteAheadCommitBeforePermitsAndSurviveCrash(t *testing.T) {
	f, dynamic := filterController(t)
	guards := f.guards.api.(*mockFilterAPI)
	var journal []state.FilterRecord
	f.writeGuardIDs = func(_ context.Context, records []state.FilterRecord) error {
		if guards.commits != 0 || dynamic.adds != 0 || len(guards.live) != 0 {
			t.Fatal("journal was written after visibility or permits")
		}
		for _, r := range records {
			if r.ID == 0 || r.GuardAddress == "" || r.Phase != state.EffectPrepared {
				t.Fatal("missing write-ahead deny identity")
			}
		}
		journal = append([]state.FilterRecord(nil), records...)
		return nil
	}
	guards.onCommit = func() {
		if len(journal) == 0 || dynamic.adds != 0 {
			t.Fatal("deny commit preceded journal or followed permits")
		}
	}
	if _, err := f.Replace(context.Background(), policyFixture(t, false)); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.plan.rules {
		if r.Persistent {
			t.Fatal("persistent object in dynamic tier")
		}
	}
	for _, r := range f.guards.rules {
		if !r.Persistent || r.Action != wf.ActionBlock || !r.HardAction {
			t.Fatal("persistent permit could survive crash")
		}
	}
	// Model documented WFP session lifetimes, not a privileged native test.
	if err := dynamic.close(); err != nil {
		t.Fatal(err)
	}
	if err := guards.close(); err != nil {
		t.Fatal(err)
	}
	if len(dynamic.live) != 0 || len(guards.live) != len(journal) || !guards.layerLive {
		t.Fatal("crash erased denies or retained permits")
	}
	p, ids := guardRecoveryFixture(t, journal)
	for _, r := range p.rules {
		if r.Action != wf.ActionBlock || ids[r.ID] == 0 {
			t.Fatal("recovery restored authority")
		}
	}
	guards.onCommit = nil
	recovery := &persistentGuards{api: guards, key: p.session, rules: make(map[wf.RuleID]*wf.Rule), ids: ids, started: true}
	for _, r := range p.rules {
		recovery.rules[r.ID] = r
	}
	if err := recovery.remove(context.Background()); err != nil || len(guards.live) != 0 || guards.layerLive {
		t.Fatal("exact deny recovery failed", err)
	}
}

func TestPersistentGuardsJournalFailureNeverReachesPermitTransaction(t *testing.T) {
	for _, lostCommit := range []bool{false, true} {
		f, dynamic := filterController(t)
		guards := f.guards.api.(*mockFilterAPI)
		if lostCommit {
			guards.fail = "commit"
		} else {
			f.writeGuardIDs = func(context.Context, []state.FilterRecord) error { return state.ErrUncertain }
		}
		if _, err := f.Replace(context.Background(), policyFixture(t, false)); !errors.Is(err, ErrPolicyUncertain) {
			t.Fatal("uncertain outcome claimed applied", err)
		}
		if dynamic.adds != 0 || dynamic.commits != 0 || len(dynamic.live) != 0 {
			t.Fatal("permits reached before acknowledged deny tier")
		}
		if lostCommit && len(guards.live) == 0 || !lostCommit && len(guards.live) != 0 {
			t.Fatal("mock did not exercise expected commit outcome")
		}
	}
}

func TestPersistentGuardUncertainAbortRequiresSeparateRecovery(t *testing.T) {
	f, dynamic := filterController(t)
	guards := f.guards.api.(*mockFilterAPI)
	if _, err := f.Replace(context.Background(), policyFixture(t, false)); err != nil {
		t.Fatal(err)
	}
	before := len(guards.live)
	guards.fail, guards.abortFailure = "check", true
	if _, err := f.Replace(context.Background(), policyFixture(t, true)); !errors.Is(err, ErrPolicyUncertain) {
		t.Fatal("uncertain abort claimed rollback", err)
	}
	if err := f.ReleaseForRecovery(); err != nil || len(dynamic.live) != 0 || len(guards.live) != before || guards.pending != nil || f.closed {
		t.Fatal("release deleted denies, retained permits, or claimed cleanup", err)
	}
	if err := f.ReleaseForRecovery(); err != nil || guards.closes != 1 || dynamic.closes != 1 {
		t.Fatal("handle release not idempotent", err)
	}
	if _, err := f.Replace(context.Background(), policyFixture(t, false)); !errors.Is(err, ErrPolicy) {
		t.Fatal("released session silently retried", err)
	}
}

func TestPersistentGuardRecoveryRejectsDuplicateZeroIDAndChangedProof(t *testing.T) {
	p := policyFixture(t, false)
	var guard state.FilterRecord
	for _, r := range p.Records() {
		if r.GuardAddress != "" {
			guard = r
			break
		}
	}
	j := state.ResourceJournal{ID: policySession, Adapter: &state.AdapterRecord{ID: policySession, LUID: 12345}, Filters: []state.FilterRecord{guard, guard}}
	if _, _, err := recoverGuardPlan(j, 8); !errors.Is(err, ErrPolicyOwnership) {
		t.Fatal("duplicate prepared ID0 accepted", err)
	}
	for _, change := range []func(*state.FilterRecord){
		func(r *state.FilterRecord) { r.GuardLocal = !r.GuardLocal },
		func(r *state.FilterRecord) { r.GuardInbound = !r.GuardInbound },
		func(r *state.FilterRecord) { r.GuardAddress = "100.64.0.99" },
		func(r *state.FilterRecord) { r.Key = policySession },
		func(r *state.FilterRecord) { r.Fingerprint = "changed" },
	} {
		changed := guard
		change(&changed)
		j.Filters = []state.FilterRecord{changed}
		if _, _, err := recoverGuardPlan(j, 8); !errors.Is(err, ErrPolicyOwnership) {
			t.Fatal("changed recovery descriptor accepted", err)
		}
	}
}

func TestPersistentGuardCleanupFencesKernelIdentityAndDynamicReferences(t *testing.T) {
	for _, changed := range []bool{false, true} {
		f, dynamic := filterController(t)
		if _, err := f.Replace(context.Background(), policyFixture(t, false)); err != nil {
			t.Fatal(err)
		}
		guards := f.guards.api.(*mockFilterAPI)
		if changed {
			for key := range guards.live {
				guards.live[key]++
				break
			}
		} else {
			// Deleting the shared sublayer must fail while dynamic refs exist.
			if err := f.guards.remove(context.Background()); err == nil || len(guards.live) == 0 {
				t.Fatal("cleanup removed referenced persistent tier")
			}
		}
		ip := &WindowsIP{api: &mockIPAPI{gone: true}, maxRows: 128}
		err := f.CloseAfterIPCleanup(context.Background(), ip)
		if changed {
			if !errors.Is(err, ErrPolicyOwnership) || len(guards.live) == 0 || guards.closes != 0 {
				t.Fatal("foreign kernel identity was removed", err)
			}
		} else if err != nil || len(guards.live) != 0 || guards.layerLive || dynamic.closes != 1 {
			t.Fatal("ordered cleanup failed", err)
		}
	}
}

func TestPersistentGuardMissingIDCannotAdoptExistingKey(t *testing.T) {
	f, dynamic := filterController(t)
	if _, err := f.Replace(context.Background(), policyFixture(t, false)); err != nil {
		t.Fatal(err)
	}
	guards := f.guards.api.(*mockFilterAPI)
	if err := dynamic.close(); err != nil {
		t.Fatal(err)
	}
	for key := range f.guards.ids {
		f.guards.ids[key] = 0
		break
	}
	if err := f.guards.remove(context.Background()); !errors.Is(err, ErrPolicyOwnership) || len(guards.live) == 0 {
		t.Fatal("prepared ID0 adopted existing object", err)
	}
	// If BFE never committed any of these effects, absence is safe cleanup.
	guards.live, guards.layerLive = nil, false
	if err := f.guards.remove(context.Background()); err != nil || !f.guards.removed {
		t.Fatal("absent prepared effects could not retire", err)
	}
}

func TestPersistentGuardNativeFlagsExcludePersistentPermits(t *testing.T) {
	p := policyFixture(t, false)
	for _, r := range p.rules {
		var arena filterArena
		native, err := marshalNativeFilter(&arena, r)
		if err != nil {
			arena.close()
			t.Fatal(err)
		}
		want := uint32(0)
		if r.HardAction {
			want |= 8
		}
		if r.Persistent {
			want |= 1
		}
		flags := native.Flags
		arena.close()
		if flags != want || r.Action == wf.ActionPermit && flags&1 != 0 {
			t.Fatal("native lifetime/action flags changed")
		}
	}
}
