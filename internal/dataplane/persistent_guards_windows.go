//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

// GuardJournalWriter must durably acknowledge allocated IDs before BFE commit.
// It must not reenter WindowsFilters. The protected store writer is supplied by
// the owner with the initiating profile/intent/journal fences.
type GuardJournalWriter func(context.Context, []state.FilterRecord) error

type persistentGuards struct {
	api                              filterAPI
	key                              windows.GUID
	rules                            map[wf.RuleID]*wf.Rule
	ids                              map[wf.RuleID]uint64
	started, failed, removed, closed bool
}

func filterMissing(err error) bool {
	return errors.Is(err, windows.Errno(windows.FWP_E_FILTER_NOT_FOUND))
}
func sublayerMissing(err error) bool {
	return errors.Is(err, windows.Errno(windows.FWP_E_SUBLAYER_NOT_FOUND))
}

func guardRecords(p FilterPlan, ids map[wf.RuleID]uint64) []state.FilterRecord {
	records := p.Records()
	out := make([]state.FilterRecord, 0, len(ids))
	for i, r := range p.rules {
		if r.Persistent {
			records[i].ID = ids[r.ID]
			out = append(out, records[i])
		}
	}
	return out
}

// ensure commits deny-only objects before any dynamic permit transaction. It
// never removes an old scope; a larger plan may only add persistent denies.
func (g *persistentGuards) ensure(ctx context.Context, p FilterPlan, write GuardJournalWriter) error {
	if g == nil || g.api == nil || write == nil || g.failed || g.removed || g.closed || p.session != g.key {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	next := make(map[wf.RuleID]*wf.Rule)
	for _, r := range p.rules {
		if r.Persistent {
			next[r.ID] = r
		}
	}
	if len(next) == 0 {
		return ErrConfiguration
	}
	for id := range g.rules {
		if next[id] == nil {
			return ErrPolicyCleanupRequired
		}
	}
	if err := beginFilterTransaction(ctx, g.api); err != nil {
		g.failed = true
		return classifyPolicyError(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = g.api.abort()
		}
	}()
	fail := func(err error) error {
		g.failed = true
		finished = true
		if g.api.abort() != nil {
			return ErrPolicyUncertain
		}
		return err
	}
	if !g.started {
		if err := g.api.sublayer(g.key); err != nil {
			return fail(classifyPolicyError(err))
		}
	} else if err := g.api.checkSublayer(g.key); err != nil {
		return fail(classifyPolicyError(err))
	}
	ids := make(map[wf.RuleID]uint64, len(next))
	added := false
	for _, r := range p.rules {
		if !r.Persistent {
			continue
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		id := g.ids[r.ID]
		if old := g.rules[r.ID]; old != nil {
			actual, err := g.api.check(old)
			if err != nil {
				return fail(classifyPolicyError(err))
			}
			if actual != id {
				return fail(ErrPolicyOwnership)
			}
		} else {
			var err error
			id, err = g.api.add(r)
			if err != nil {
				return fail(classifyPolicyError(err))
			}
			if id == 0 {
				return fail(ErrPolicy)
			}
			added = true
		}
		actual, err := g.api.check(r)
		if err != nil {
			return fail(classifyPolicyError(err))
		}
		if actual != id {
			return fail(ErrPolicyOwnership)
		}
		ids[r.ID] = id
	}
	if added {
		if err := write(ctx, guardRecords(p, ids)); err != nil {
			return fail(errors.Join(ErrPolicyUncertain, err))
		}
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	// Keep allocated identities even on a lost commit response. They were
	// written ahead, so recovery can distinguish the original object from a
	// same-key replacement without minting traffic authority.
	g.rules, g.ids = next, ids
	if g.api.commit() != nil {
		_ = g.api.abort()
		finished = true
		g.failed = true
		return ErrPolicyUncertain
	}
	finished = true
	g.started = true
	if ctx.Err() != nil {
		g.failed = true
		return ErrPolicyUncertain
	}
	return nil
}

// remove is called only after the dynamic session and native IP effects are
// gone. Every delete is guarded by exact attributes and the journal kernel ID.
func (g *persistentGuards) remove(ctx context.Context) error {
	if g.removed {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := beginFilterTransaction(ctx, g.api); err != nil {
		return classifyPolicyError(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = g.api.abort()
		}
	}()
	fail := func(err error) error {
		finished = true
		if g.api.abort() != nil {
			return ErrPolicyUncertain
		}
		return err
	}
	err := g.api.checkSublayer(g.key)
	missing := sublayerMissing(err)
	if err != nil && !missing {
		return fail(classifyPolicyError(err))
	}
	for id, r := range g.rules {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		actual, err := g.api.check(r)
		if filterMissing(err) {
			continue
		}
		if err != nil {
			return fail(classifyPolicyError(err))
		}
		if missing || g.ids[id] == 0 || actual != g.ids[id] {
			return fail(ErrPolicyOwnership)
		}
		if err := g.api.remove(id); err != nil {
			return fail(classifyPolicyError(err))
		}
	}
	if !missing {
		if err := g.api.removeSublayer(g.key); err != nil {
			return fail(classifyPolicyError(err))
		}
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if g.api.commit() != nil {
		_ = g.api.abort()
		finished = true
		return ErrPolicyUncertain
	}
	finished = true
	g.removed = true
	return nil
}
func (g *persistentGuards) close() error {
	if g.closed {
		return nil
	}
	if err := g.api.close(); err != nil {
		return ErrPolicyUncertain
	}
	g.closed = true
	return nil
}

func recoverGuardPlan(j state.ResourceJournal, maxProtected int) (FilterPlan, map[wf.RuleID]uint64, error) {
	key, err := canonicalGUID(j.ID)
	if err != nil || j.Adapter == nil || j.Adapter.ID != j.ID || j.Adapter.LUID == 0 || maxProtected <= 0 {
		return FilterPlan{}, nil, ErrConfiguration
	}
	p := FilterPlan{session: key, luid: j.Adapter.LUID, maxProtected: maxProtected, scopes: make(map[ipGuardScope]bool)}
	ids := make(map[wf.RuleID]uint64)
	for _, record := range j.Filters {
		if record.GuardAddress == "" {
			continue
		}
		ip, err := netip.ParseAddr(record.GuardAddress)
		if err != nil || !hostIPv4(ip) || ip.String() != record.GuardAddress {
			return FilterPlan{}, nil, ErrPolicyOwnership
		}
		field := wf.FieldIPRemoteAddress
		if record.GuardLocal {
			field = wf.FieldIPLocalAddress
		}
		layer := wf.LayerOutboundIPPacketV4
		if record.GuardInbound {
			layer = wf.LayerInboundIPPacketV4
		}
		scope := ipGuardScope{field: field, address: ip}
		p.scopes[scope] = true
		if len(p.scopes) > maxProtected {
			return FilterPlan{}, nil, ErrPolicyLimit
		}
		r := &wf.Rule{Layer: layer, Action: wf.ActionBlock, HardAction: true, Persistent: true, Weight: 1, Conditions: []*wf.Match{{Field: field, Op: wf.MatchTypeEqual, Value: ip}}}
		p.addRule(r)
		expected := p.Records()[len(p.rules)-1]
		_, duplicate := ids[r.ID]
		if expected.Key != record.Key || expected.Fingerprint != record.Fingerprint || duplicate {
			return FilterPlan{}, nil, ErrPolicyOwnership
		}
		ids[r.ID] = record.ID
	}
	p.canonicalize()
	return p, ids, nil
}

// RecoverWindowsGuardCleanup never restores dynamic permits from cached state.
// Its only permitted operation is cleanup after native absence is confirmed.
func RecoverWindowsGuardCleanup(ctx context.Context, j state.ResourceJournal, wait time.Duration, maxProtected int) (*WindowsFilters, error) {
	if ctx == nil {
		return nil, ErrConfiguration
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	p, ids, err := recoverGuardPlan(j, maxProtected)
	if err != nil {
		return nil, err
	}
	api, err := openFilterAPI(wait, false)
	if err != nil {
		return nil, err
	}
	g := &persistentGuards{api: api, key: p.session, rules: make(map[wf.RuleID]*wf.Rule), ids: ids, started: true}
	for _, r := range p.rules {
		g.rules[r.ID] = r
	}
	// Read/compare happens in remove's transaction, immediately before delete.
	return &WindowsFilters{session: p.session, guards: g, attempted: &p, recoveryOnly: true, dynamicClosed: true}, nil
}
