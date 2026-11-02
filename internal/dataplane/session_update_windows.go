//go:build windows

package dataplane

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
)

func (s *WindowsSession) updateContext(parent context.Context) (context.Context, func(), error) {
	if parent == nil {
		return nil, nil, ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(parent, s.config.ApplyTimeout)
	stop := context.AfterFunc(s.lifetime, cancel)
	finish := func() { stop(); cancel() }
	if err := s.lifetime.Err(); err != nil {
		finish()
		return nil, nil, err
	}
	return ctx, finish, nil
}

func (s *WindowsSession) failUpdate(err error) error {
	s.lease.Revoke()
	s.cancel()
	s.stopped = true
	return errors.Join(err, s.releaseHandles())
}

func (s *WindowsSession) retainedPlan(ctx context.Context, m connectivity.Map, j state.ResourceJournal, offline bool) (FilterPlan, error) {
	row, err := s.identity(j.Adapter.LUID)
	if err != nil {
		return FilterPlan{}, adapterError(err)
	}
	if row.Index == 0 || adapterProof(row) != j.Adapter.Fingerprint {
		return FilterPlan{}, ErrAdapterOwnership
	}
	p, err := s.planFilters(ctx, m, s.id, j.Adapter.LUID, row.Index, s.config.MaxGrants, s.config.MaxProtected, offline)
	if err != nil {
		return FilterPlan{}, err
	}
	p, err = p.RetainIPGuards(j)
	if err != nil {
		return FilterPlan{}, err
	}
	// A previously removed route may no longer have a journal row, while its
	// deny remains until full session teardown. Retain that native scope too.
	for scope := range s.policy.scopes {
		if err := p.protect(scope.field, scope.address); err != nil {
			return FilterPlan{}, err
		}
	}
	p.canonicalize()
	return p, nil
}

// Refresh and Offline are delivered by one serialized authority worker; Stop
// may run concurrently and revokes the Lease before waiting for either method.
// Unchanged peers retain their actual WG objects and established sessions.
func (s *WindowsSession) Refresh(parent context.Context, m connectivity.Map) (err error) {
	if m.Denied() {
		s.lease.Revoke()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == nil {
		return ErrConfiguration
	}
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if m.Cursor() == s.current.Cursor() {
		return connectivity.ErrReplay
	}
	sourceEpoch := s.sourceEpoch(parent)
	s.lease.PausePackets(true)
	defer func() {
		if err != nil {
			err = s.failUpdate(err)
		}
	}()
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return err
	}
	defer finish()
	p, err := s.profile(ctx, m, false)
	if err != nil {
		return err
	}
	defer clear(p.Keys.WireGuardPrivate)
	if err := connectivity.PersistCursor(ctx, s.store, s.generation, m); err != nil {
		return err
	}
	if err := s.lease.Confirm(m); err != nil {
		return err
	}
	ipPlan, err := PrepareIPPlan(m, s.id, s.tunnel.Record().LUID, s.config.RouteMetric, s.config.MaxPeers)
	if err != nil {
		return err
	}
	j, err := s.change(ctx, m, func(*state.ResourceJournal) error { return nil })
	if err != nil {
		return err
	}
	a, desired := ipPlan.Prepared()
	if len(j.Addresses) != 1 || j.Addresses[0].Prefix != a.Prefix || j.Addresses[0].Phase != state.EffectApplied {
		return ErrIPConflict
	}
	effect, err := s.ip.InspectAddress(ctx, ipPlan, j.Addresses[0])
	if err != nil {
		return err
	}
	if !effect.Ready {
		return ErrIPConflict
	}
	j, err = s.change(ctx, m, func(j *state.ResourceJournal) error { j.Addresses[0] = effect.Record; return nil })
	if err != nil {
		return err
	}
	if err := s.ip.Preflight(ctx, ipPlan, *j); err != nil {
		return err
	}
	plan, err := s.retainedPlan(ctx, m, *j, false)
	if err != nil {
		return err
	}
	// Atomic replacement retains unchanged permit IDs. An intermediate deny-all
	// would reauthorize/terminate unchanged TCP flows, so it is not used here.
	if err := s.replaceFilters(ctx, m, plan); err != nil {
		return err
	}
	wanted := make(map[string]state.RouteRecord, len(desired))
	for _, r := range desired {
		wanted[r.Destination] = r
	}
	for _, old := range j.Routes {
		if _, keep := wanted[old.Destination]; keep {
			if old.Phase != state.EffectApplied {
				return ErrIPOwnership
			}
			if err := s.ip.inspectRoute(ctx, ipPlan, old); err != nil {
				return err
			}
			continue
		}
		removing := old
		removing.Phase = state.EffectRemoving
		j, err = s.change(ctx, m, func(j *state.ResourceJournal) error {
			for i, r := range j.Routes {
				if r.Destination == old.Destination {
					j.Routes[i] = removing
					return nil
				}
			}
			return state.ErrStale
		})
		if err != nil {
			return err
		}
		cleanup, err := PrepareIPCleanup(s.id, j.Adapter.LUID, old.Destination)
		if err != nil {
			return err
		}
		if err := s.ip.RemoveRoute(ctx, cleanup, removing); err != nil {
			return err
		}
		j, err = s.change(ctx, m, func(j *state.ResourceJournal) error {
			for i, r := range j.Routes {
				if r.Destination == old.Destination {
					j.Routes = append(j.Routes[:i], j.Routes[i+1:]...)
					return nil
				}
			}
			return state.ErrStale
		})
		if err != nil {
			return err
		}
	}
	for _, route := range desired {
		found := false
		for _, old := range j.Routes {
			if old.Destination == route.Destination {
				found = true
				break
			}
		}
		if found {
			continue
		}
		j, err = s.change(ctx, m, func(j *state.ResourceJournal) error { j.Routes = append(j.Routes, route); return nil })
		if err != nil {
			return err
		}
		receipt, err := s.ip.CreateRoute(ctx, ipPlan, route, *j)
		if err != nil {
			return err
		}
		j, err = s.change(ctx, m, func(j *state.ResourceJournal) error {
			for i, r := range j.Routes {
				if r.Destination == route.Destination {
					j.Routes[i] = receipt
					return nil
				}
			}
			return state.ErrStale
		})
		if err != nil {
			return err
		}
	}
	current, err := s.profile(ctx, m, true)
	if err != nil {
		return err
	}
	defer clear(current.Keys.WireGuardPrivate)
	if err := s.engine.Apply(ctx, m, current.Keys.WireGuardPrivate); err != nil {
		return err
	}
	if err := s.activationFence(ctx, m); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.current, s.policy = m, plan
	resumed, err := s.resumeSource(sourceEpoch)
	if err != nil {
		return err
	}
	if !resumed {
		return nil
	}
	if !s.lease.Permissions().NewFlows {
		return connectivity.ErrExpired
	}
	return nil
}

func (s *WindowsSession) Offline(parent context.Context) (err error) {
	var sourceEpoch uint64
	if parent != nil {
		if epoch, ok := parent.Value(sourceEpochKey{}).(uint64); ok {
			sourceEpoch = epoch
		} else {
			sourceEpoch = s.BlockSource()
		}
	} else {
		sourceEpoch = s.BlockSource()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == nil {
		return ErrConfiguration
	}
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	s.lease.PausePackets(true)
	s.lease.Unavailable()
	defer func() {
		if err != nil {
			err = s.failUpdate(err)
		}
	}()
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return err
	}
	defer finish()
	j, err := s.change(ctx, s.current, func(*state.ResourceJournal) error { return nil })
	if err != nil {
		return err
	}
	plan, err := s.retainedPlan(ctx, s.current, *j, true)
	if err != nil {
		return err
	}
	if err := s.replaceFilters(ctx, s.current, plan); err != nil {
		return err
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.policy = plan
	resumed, err := s.resumeSource(sourceEpoch)
	if err != nil {
		return err
	}
	if !resumed {
		return nil
	}
	if !s.lease.Permissions().ExistingFlows {
		return connectivity.ErrExpired
	}
	return nil
}
