//go:build windows

package dataplane

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
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

// Freeze the installed rule set against signed time at the native boundary.
// Preparing the journal and committing WFP can outlast a short-lived grant.
func (s *WindowsSession) installCurrentFilters(ctx context.Context, m connectivity.Map, plan *FilterPlan) error {
	if !plan.nextGrantEnd().IsZero() {
		at, err := s.grantTime(m)
		if err != nil {
			return err
		}
		*plan = plan.currentGrants(at)
	}
	for {
		if err := s.replaceFilters(ctx, m, *plan); err != nil {
			return err
		}
		end := plan.nextGrantEnd()
		if end.IsZero() {
			return nil
		}
		at, err := s.grantTime(m)
		if err != nil {
			return err
		}
		if at.Before(end) {
			return nil
		}
		*plan = plan.currentGrants(at)
	}
}

func (s *WindowsSession) currentFilterPlan(ctx context.Context, m connectivity.Map, luid uint64, index uint32, offline bool) (FilterPlan, error) {
	p, err := s.planFilters(ctx, m, s.id, luid, index, s.config.MaxGrants, s.config.MaxProtected, offline)
	if err != nil {
		return FilterPlan{}, err
	}
	at, err := s.grantTime(m)
	if err != nil {
		return FilterPlan{}, err
	}
	return p.currentGrants(at), nil
}

func (s *WindowsSession) grantTime(m connectivity.Map) (time.Time, error) {
	sample, err := s.clock.Sample()
	if err != nil {
		return time.Time{}, connectivity.ErrClock
	}
	advance, err := m.WallDeadlineAdvance(sample)
	if err != nil {
		return time.Time{}, err
	}
	at := sample.Wall.Add(advance)
	cursor := m.Cursor()
	if cursor == s.grantClockCursor && at.Before(s.grantClockFloor) {
		return time.Time{}, connectivity.ErrClock
	}
	s.grantClockCursor, s.grantClockFloor = cursor, at
	return at, nil
}

// Called under the session lock. Timer callbacks from a replaced map or a
// previous online/offline plan may already be waiting for this lock.
func (s *WindowsSession) cancelGrantExpiry() {
	s.grantTimerEpoch++
	if s.grantTimer != nil {
		s.grantTimer.Stop()
		s.grantTimer = nil
	}
}

// Apply cutoffs already reached before releasing the session lock. A native
// commit can span a second cutoff, so sample again after each removal.
func (s *WindowsSession) scheduleGrantExpiry(ctx context.Context) error {
	s.cancelGrantExpiry()
	for {
		end := s.policy.nextGrantEnd()
		if end.IsZero() {
			return nil
		}
		now, err := s.grantTime(s.current)
		if err != nil {
			return err
		}
		if now.Before(end) {
			epoch := s.grantTimerEpoch
			s.grantTimer = time.AfterFunc(end.Sub(now), func() { s.onGrantTimer(epoch) })
			return nil
		}
		next := s.policy.currentGrants(now)
		if err := s.replaceFilters(ctx, s.current, next); err != nil {
			return err
		}
		s.policy = next
	}
}

func (s *WindowsSession) onGrantTimer(epoch uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.grantTimerEpoch || s.stopped || s.lifetime.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.lifetime, s.config.ApplyTimeout)
	defer cancel()
	if err := s.scheduleGrantExpiry(ctx); err != nil {
		err = s.failUpdate(err)
		if report := s.config.ReportFailure; report != nil {
			report(s.generation, "apply_grant_expiry", err)
		}
	}
}

func (s *WindowsSession) retainedPlan(ctx context.Context, m connectivity.Map, j state.ResourceJournal, offline bool) (FilterPlan, error) {
	row, err := s.identity(j.Adapter.LUID)
	if err != nil {
		return FilterPlan{}, adapterError(err)
	}
	if row.Index == 0 || adapterProof(row) != j.Adapter.Fingerprint {
		return FilterPlan{}, ErrAdapterOwnership
	}
	p, err := s.currentFilterPlan(ctx, m, j.Adapter.LUID, row.Index, offline)
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
	if len(s.policy.direct) == 0 || m.Denied() {
		return p, nil
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		return FilterPlan{}, err
	}
	sample, err := s.clock.Sample()
	if err != nil {
		return FilterPlan{}, connectivity.ErrClock
	}
	previousMap, err := s.current.Snapshot()
	if err != nil {
		return FilterPlan{}, err
	}
	oldPeers := make(map[string]coordinator.Peer, len(previousMap.Peers))
	for _, peer := range previousMap.Peers {
		oldPeers[peer.ID] = peer
	}
	peers := make(map[string]coordinator.Peer, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		peers[peer.ID] = peer
	}
	for luid, previous := range s.policy.direct {
		a, err := offeredAssignment(s.current, s.lease, previousMap.Node, oldPeers[previous.peerID], previous.profileID)
		if err != nil {
			return FilterPlan{}, ErrNoAuthority
		}
		peer, ok := peers[previous.peerID]
		if !ok {
			continue
		}
		current, err := signedPeerAssignment(m, s.lease, snapshot, peer, a.offer.Profile.ID, sample.Wall)
		if err != nil || !a.keepPair(previous.localEndpoint, previous.peerEndpoint) || !current.keepPair(previous.localEndpoint, previous.peerEndpoint) || !a.sameProbe(current) {
			continue
		}
		identity, err := s.identity(luid)
		if err != nil || identity.GUID != previous.adapter || identity.LUID != luid || identity.Index != previous.index {
			return FilterPlan{}, ErrAdapterOwnership
		}
		next, err := p.withConfirmedPeer(current, identity, offline)
		if errors.Is(err, ErrNoAuthority) {
			continue
		}
		if err != nil {
			return FilterPlan{}, err
		}
		p = next
	}
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
	if ipPlan.exit {
		if s.outer == nil {
			return ErrConfiguration
		}
		if err := s.outer.pin(); err != nil {
			return err
		}
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
	if err := s.installCurrentFilters(ctx, m, &plan); err != nil {
		return err
	}
	if err := s.retireUnconfirmedDirectRoutes(ctx, plan, *j); err != nil {
		return err
	}
	wanted := make(map[string]state.RouteRecord, len(desired))
	for _, r := range desired {
		wanted[r.Destination] = r
	}
	removedResource := false
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
		if err := s.ip.RemoveRoute(ctx, ipPlan, removing); err != nil {
			return err
		}
		removedResource = removedResource || old.Resource
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
	if err := s.applyObservedPaths(ctx, m, current.Keys.WireGuardPrivate); err != nil {
		return err
	}
	// Keep withdrawn resource guards until the route deletion receipts and
	// WireGuard replacement are complete. Then release them in this refresh.
	if removedResource {
		plan, err = s.retainedPlan(ctx, m, *j, false)
		if err != nil {
			return err
		}
		if err := s.installCurrentFilters(ctx, m, &plan); err != nil {
			return err
		}
	}
	if err := s.applyDNS(ctx, m); err != nil {
		return err
	}
	if err := s.activationFence(ctx, m); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.current, s.policy = m, plan
	if err := s.scheduleGrantExpiry(ctx); err != nil {
		return err
	}
	s.publishAppliedView()
	if s.supervisor != nil {
		s.supervisor.Update(m)
	}
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
	return connectivity.RecordAppliedMap(ctx, s.store, s.generation, m)
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
	if len(s.policy.direct) != 0 {
		ipPlan, err := PrepareIPPlan(s.current, s.id, j.Adapter.LUID, s.config.RouteMetric, s.config.MaxPeers)
		if err != nil {
			return err
		}
		if err := s.ip.Preflight(ctx, ipPlan, *j); err != nil {
			return err
		}
	}
	plan, err := s.retainedPlan(ctx, s.current, *j, true)
	if err != nil {
		return err
	}
	if err := s.installCurrentFilters(ctx, s.current, &plan); err != nil {
		return err
	}
	if err := s.retireUnconfirmedDirectRoutes(ctx, plan, *j); err != nil {
		return err
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.policy = plan
	if err := s.scheduleGrantExpiry(ctx); err != nil {
		return err
	}
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

func (s *WindowsSession) applyObservedPaths(ctx context.Context, m connectivity.Map, key []byte) error {
	snapshot, err := m.Snapshot()
	if err != nil {
		return err
	}
	sample, err := s.clock.Sample()
	if err != nil {
		return err
	}
	paths, err := observedResourcePaths(snapshot, s.resourceEvidence, sample)
	if err != nil {
		return err
	}
	if err := s.packets.updatePaths(m, paths); err != nil {
		return err
	}
	return s.engine.applyPaths(ctx, snapshot, key, paths)
}
