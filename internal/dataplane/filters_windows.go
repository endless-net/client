//go:build windows

package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

type filterAPI interface {
	begin() error
	commit() error
	abort() error
	sublayer(windows.GUID) error
	checkSublayer(windows.GUID) error
	removeSublayer(windows.GUID) error
	add(*wf.Rule) (uint64, error)
	check(*wf.Rule) (uint64, error)
	remove(wf.RuleID) error
	close() error
}

// WindowsFilters owns persistent deny effects and a dynamic permit session,
// never the system's rule table.
// Its caller must persist the plan's prepared records and fence current intent
// before Replace, and disable the tunnel on any error/uncertain result. A filter
// update is not proof of an active/reachable WireGuard path.
type WindowsFilters struct {
	mu                          sync.Mutex
	api                         filterAPI
	guards                      *persistentGuards
	writeGuardIDs               GuardJournalWriter
	recoveryOnly, dynamicClosed bool
	session                     windows.GUID
	plan                        *FilterPlan
	attempted                   *FilterPlan
	ids                         map[wf.RuleID]uint64
	failed, closed              bool
}

func (*WindowsFilters) String() string   { return "[Windows filter session]" }
func (*WindowsFilters) GoString() string { return "[Windows filter session]" }

func NewWindowsFilters(sessionID string, transactionWait time.Duration, writeGuardIDs GuardJournalWriter) (*WindowsFilters, error) {
	if writeGuardIDs == nil {
		return nil, ErrConfiguration
	}
	key, err := canonicalGUID(sessionID)
	if err != nil {
		return nil, err
	}
	guardAPI, err := openFilterAPI(transactionWait, false)
	if err != nil {
		return nil, err
	}
	api, err := openFilterAPI(transactionWait, true)
	if err != nil {
		_ = guardAPI.close()
		return nil, err
	}
	return &WindowsFilters{api: api, session: key, guards: &persistentGuards{api: guardAPI, key: key}, writeGuardIDs: writeGuardIDs}, nil
}

// Replace checks exact previous ownership inside the same write transaction as
// deletion/insertion. Persistent denies commit before dynamic permits; the
// dynamic policy replacement itself becomes visible atomically.
// No reflective access to another library's private session handle is used.
func (f *WindowsFilters) Replace(ctx context.Context, p FilterPlan) ([]state.FilterRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.api == nil || f.guards == nil || f.writeGuardIDs == nil || f.recoveryOnly {
		return nil, ErrConfiguration
	}
	if f.closed || f.failed {
		return nil, ErrPolicy
	}
	if ctx == nil || p.session != f.session || p.luid == 0 || p.index == 0 || len(p.rules) < 2 || f.plan != nil && p.luid != f.plan.luid {
		return nil, ErrConfiguration
	}
	if f.plan != nil {
		for scope := range f.plan.scopes {
			if !p.scopes[scope] {
				return nil, ErrPolicyCleanupRequired
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f.attempted = &p
	if err := f.guards.ensure(ctx, p, f.writeGuardIDs); err != nil {
		f.failed = true
		return nil, err
	}
	full := p
	p.rules = make([]*wf.Rule, 0, len(full.rules))
	for _, r := range full.rules {
		if !r.Persistent {
			p.rules = append(p.rules, r)
		}
	}
	if err := beginFilterTransaction(ctx, f.api); err != nil {
		f.failed = true
		return nil, classifyPolicyError(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = f.api.abort()
		}
	}()
	fail := func(cause error) ([]state.FilterRecord, error) {
		f.failed = true
		finished = true
		if f.api.abort() != nil {
			return nil, ErrPolicyUncertain
		}
		return nil, cause
	}
	if err := f.api.checkSublayer(f.session); err != nil {
		return fail(classifyPolicyError(err))
	}
	next := make(map[wf.RuleID]*wf.Rule, len(p.rules))
	for _, r := range p.rules {
		next[r.ID] = r
	}
	old := make(map[wf.RuleID]bool, len(f.ids))
	if f.plan != nil {
		for _, r := range f.plan.rules {
			if ctx.Err() != nil {
				return fail(ctx.Err())
			}
			id, err := f.api.check(r)
			if err != nil {
				return fail(classifyPolicyError(err))
			}
			if id != f.ids[r.ID] {
				return fail(ErrPolicyOwnership)
			}
			old[r.ID] = true
			if next[r.ID] == nil {
				if err := f.api.remove(r.ID); err != nil {
					return fail(classifyPolicyError(err))
				}
			}
		}
	}
	ids := make(map[wf.RuleID]uint64, len(p.rules))
	for _, r := range p.rules {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		id := f.ids[r.ID]
		if !old[r.ID] {
			var err error
			id, err = f.api.add(r)
			if err != nil {
				return fail(classifyPolicyError(err))
			}
			if id == 0 {
				return fail(ErrPolicy)
			}
		}
		actualID, err := f.api.check(r)
		if err != nil {
			return fail(classifyPolicyError(err))
		}
		if actualID != id {
			return fail(ErrPolicyOwnership)
		}
		ids[r.ID] = id
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if f.api.commit() != nil {
		// A lost BFE response cannot prove rollback. Abort is best-effort and
		// does not turn commit uncertainty into a safe retry or success.
		_ = f.api.abort()
		finished = true
		f.failed = true
		return nil, ErrPolicyUncertain
	}
	finished = true
	f.plan, f.ids = &p, ids
	if ctx.Err() != nil {
		f.failed = true
		return nil, ErrPolicyUncertain
	}
	records := full.Records()
	for i, r := range full.rules {
		id := ids[r.ID]
		if r.Persistent {
			id = f.guards.ids[r.ID]
		}
		records[i].ID = id
		records[i].Phase = state.EffectApplied
	}
	return records, nil
}

func (f *WindowsFilters) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	if f.api == nil && f.guards == nil {
		f.closed = true
		return nil
	}
	if f.attempted != nil {
		return ErrPolicyCleanupRequired
	}
	return f.close(context.Background())
}

// ReleaseForRecovery drops this process's permits and closes management handles
// without deleting persistent denies. It is the terminal escape from uncertain
// transactions: closing a non-dynamic engine also aborts its pending transaction.
// Success acknowledges handle release only, never native resource cleanup. A
// separate cleanup owner must reload the protected journal and compare objects.
func (f *WindowsFilters) ReleaseForRecovery() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = true
	if !f.dynamicClosed && f.api != nil {
		if f.api.close() != nil {
			return ErrPolicyUncertain
		}
		f.dynamicClosed = true
	}
	if f.guards != nil {
		return f.guards.close()
	}
	return nil
}

// CloseAfterIPCleanup queries actual native state; a caller-provided boolean or
// cancelled deadline is not evidence that routes/addresses/adapter are absent.
func (f *WindowsFilters) CloseAfterIPCleanup(ctx context.Context, ip *WindowsIP) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	if ctx == nil || ip == nil || f.guards == nil {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.attempted != nil {
		if err := ip.confirmFilterCleanup(ctx, *f.attempted); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ErrPolicyUncertain
	}
	return f.close(ctx)
}

func (f *WindowsFilters) close(ctx context.Context) error {
	if !f.dynamicClosed {
		if f.api == nil || f.api.close() != nil {
			f.failed = true
			return ErrPolicyUncertain
		}
		f.dynamicClosed = true
	}
	if f.guards != nil {
		if f.attempted != nil {
			if err := f.guards.remove(ctx); err != nil {
				return err
			}
		}
		if err := f.guards.close(); err != nil {
			return err
		}
	}
	f.closed = true
	return nil
}

// FWP_E_TIMEOUT means the transaction lock was not acquired, so no native
// mutation has started. Continue that lock wait only within the caller's
// original bounded context. Never replay Add/Delete/Commit or an unknown error.
func beginFilterTransaction(ctx context.Context, api filterAPI) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := api.begin()
		if err == nil {
			if cancelled := ctx.Err(); cancelled != nil {
				if api.abort() != nil {
					return ErrPolicyUncertain
				}
				return cancelled
			}
			return nil
		}
		if !errors.Is(err, windows.Errno(windows.FWP_E_TIMEOUT)) {
			return err
		}
		if _, bounded := ctx.Deadline(); !bounded {
			return err
		}
	}
}
