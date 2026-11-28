//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func (s *WindowsSession) selectDirect(route *peerRoute, token *directActivation) (*assignedCipher, error) {
	if token == nil || token.direct == nil {
		return nil, ErrNoAuthority
	}
	engine, native := token.direct.cipher.(*windowsDirectEngine)
	if !native {
		return route.SelectDirect(token)
	}
	if err := token.check(); err != nil {
		return nil, err
	}
	engine.routeMu.Lock()
	defer engine.routeMu.Unlock()
	d := engine.device
	if engine.channel != token.direct || d.Adapter == nil || d.probeGuard == nil || engine.retireRoute != nil {
		return nil, ErrAdapterOwnership
	}
	b := engine.channel.bind
	b.mu.Lock()
	a := b.assignment
	b.mu.Unlock()
	_, peer, err := a.addresses()
	if err != nil {
		return nil, err
	}
	row, err := d.LUID().Interface()
	if err != nil {
		return nil, err
	}
	id := adapterInfo(*row)
	if err := d.prepareApplicationIP(id); err != nil {
		return nil, err
	}
	var previous *assignedCipher
	entry, err := s.stageDirectIP(b.ctx, a, id, func() error {
		var commitErr error
		previous, commitErr = route.selectDirect(token, func() error {
			// The channel and bind are locked through publication of both
			// directions. A queued driver write must pass the new admission.
			if err := b.checkAdapterLocked(); err != nil {
				return err
			}
			if !b.assignment.sameProbe(a) || b.assignment.mapValue.Cursor() != a.mapValue.Cursor() {
				return connectivity.ErrSourceChanged
			}
			if err := d.probeGuard.close(); err != nil {
				return err
			}
			d.probeGuard = nil
			b.adapterProbeOnly = false
			return nil
		})
		return commitErr
	})
	if entry != nil {
		engine.retireRoute = func() {
			if err := s.retireDirectIP(context.WithoutCancel(b.ctx), peer, entry); err != nil {
				s.ip.mu.Lock()
				stillOwned := s.ip.direct[peer] == entry
				s.ip.mu.Unlock()
				if stillOwned {
					s.lease.Revoke()
					s.cancel()
				}
			}
		}
	}
	return previous, err
}

// Called under s.mu. A pre-existing WeakHostSend setting is never claimed.
func (s *WindowsSession) setPrimaryDirectSource(enabled bool) error {
	if !enabled && !s.weakSendOwned {
		return nil
	}
	record := s.tunnel.Record()
	id, err := s.identity(record.LUID)
	if err != nil || id.LUID != record.LUID || adapterProof(id) != record.Fingerprint {
		return ErrAdapterOwnership
	}
	changed, err := s.ip.api.setWeakSend(id, enabled)
	if enabled && changed {
		s.weakSendOwned = true
	} else if !enabled && err == nil {
		s.weakSendOwned = false
	}
	return err
}

func (s *WindowsSession) retireUnconfirmedDirectRoutes(ctx context.Context, next FilterPlan, j state.ResourceJournal) error {
	if err := s.ip.retireUnconfirmedDirectRoutes(ctx, next, j); err != nil {
		return err
	}
	if len(next.direct) == 0 {
		return s.setPrimaryDirectSource(false)
	}
	return nil
}

// stageDirectIP installs the pair's policy and preferred host route while the
// caller keeps the candidate's probe guard active and owns its adapter handle.
// A supervisor must yield to a concurrent Refresh or Stop, which may itself
// be waiting for the supervisor.
func (s *WindowsSession) stageDirectIP(parent context.Context, assignment candidateAssignment, candidate adapterIdentity, commit func() error) (*windowsDirectRoute, error) {
	if !s.mu.TryLock() {
		return nil, connectivity.ErrSourceChanged
	}
	defer s.mu.Unlock()
	if s.stopped || s.tunnel == nil || s.filters == nil {
		return nil, ErrClosed
	}
	m := assignment.mapValue
	if assignment.offer.Profile.Engine != transport.WireGuard || assignment.plan.LocalProbeIPv4 == "" || assignment.plan.PeerProbeIPv4 == "" {
		return nil, ErrNoAuthority
	}
	_, peer, err := assignment.addresses()
	if err != nil {
		return nil, err
	}
	_, changed := s.lease.SourceSignal()
	currentAuthority := func() bool {
		select {
		case <-changed:
			return false
		default:
			sample, err := s.clock.Sample()
			return err == nil && assignment.lease == s.lease && s.current.Cursor() == m.Cursor() && assignment.permission(sample.Wall, true) == nil
		}
	}
	if !currentAuthority() {
		return nil, ErrNoAuthority
	}
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return nil, err
	}
	defer finish()
	primary, err := PrepareIPPlan(m, s.id, s.tunnel.Record().LUID, s.config.RouteMetric, s.config.MaxPeers)
	if err != nil {
		return nil, err
	}
	current, err := s.identity(candidate.LUID)
	if err != nil || current != candidate {
		return nil, ErrAdapterOwnership
	}
	profile, err := s.profile(ctx, m, true)
	if err != nil {
		return nil, err
	}
	clear(profile.Keys.WireGuardPrivate)
	j := profile.Connection.Journal
	if j == nil || j.Generation != s.generation {
		return nil, ErrIPOwnership
	}
	at, err := s.grantTime(m)
	if err != nil {
		return nil, err
	}
	next, err := s.policy.currentGrants(at).withConfirmedPeer(assignment, candidate, false)
	if err != nil {
		return nil, err
	}
	err = s.installCurrentFilters(ctx, m, &next)
	var entry *windowsDirectRoute
	if err == nil && !currentAuthority() {
		err = ErrNoAuthority
	}
	// Windows selects the route together with its usable source. Allow the
	// primary address before checking which interface wins. The candidate's
	// probe guard still blocks application traffic until commit succeeds.
	if err == nil {
		err = s.setPrimaryDirectSource(true)
	}
	if err == nil {
		entry, err = s.ip.installDirectRoute(ctx, primary, *j, candidate.GUID, winipcfg.LUID(candidate.LUID), peer)
	}
	if err == nil {
		switch {
		case ctx.Err() != nil:
			err = ctx.Err()
		case !currentAuthority():
			err = ErrNoAuthority
		default:
			if commit != nil {
				err = commit()
			}
			if err == nil {
				s.policy = next
				return entry, nil
			}
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.ApplyTimeout)
	defer cancel()
	restore := s.policy
	at, clockErr := s.grantTime(m)
	if clockErr != nil || !currentAuthority() {
		restore = restore.DenyAllTraffic()
	} else {
		restore = restore.currentGrants(at)
	}
	restoreErr := s.installCurrentFilters(cleanup, m, &restore)
	if restoreErr == nil {
		s.policy = restore
	}
	if entry != nil {
		if retireErr := s.ip.retireDirectRoute(cleanup, peer, entry); retireErr != nil {
			restoreErr = errors.Join(restoreErr, retireErr)
		} else {
			entry = nil
		}
	}
	if len(restore.direct) == 0 {
		restoreErr = errors.Join(restoreErr, s.setPrimaryDirectSource(false))
	}
	if restoreErr != nil {
		s.lease.Revoke()
		s.cancel()
	}
	return entry, errors.Join(err, restoreErr)
}

// retireDirectIP removes this candidate's grants before restoring the primary
// route. A delayed close must never remove a replacement's filter exceptions.
func (s *WindowsSession) retireDirectIP(parent context.Context, peer netip.Addr, entry *windowsDirectRoute) error {
	if !s.mu.TryLock() {
		return connectivity.ErrSourceChanged
	}
	defer s.mu.Unlock()
	if s.stopped || s.tunnel == nil || s.filters == nil {
		return ErrClosed
	}
	s.ip.mu.Lock()
	owned := entry != nil && s.ip.direct[peer] == entry && entry.generation == s.generation &&
		entry.primaryGUID == s.policy.session && uint64(entry.primaryLUID) == s.policy.luid
	s.ip.mu.Unlock()
	if !owned {
		return ErrIPOwnership
	}
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return err
	}
	defer finish()
	next, err := s.policy.withoutDirectPeer(peer, uint64(entry.candidate.luid))
	if err != nil {
		return err
	}
	if !s.lease.Permissions().ExistingFlows {
		next = next.DenyAllTraffic()
	}
	if err := s.installCurrentFilters(ctx, s.current, &next); err != nil {
		s.lease.Revoke()
		s.cancel()
		return err
	}
	s.policy = next
	if err := s.ip.retireDirectRoute(ctx, peer, entry); err != nil {
		return err
	}
	if len(next.direct) == 0 {
		if err := s.setPrimaryDirectSource(false); err != nil {
			s.lease.Revoke()
			s.cancel()
			return err
		}
	}
	return nil
}

// CheckReady fences local activation after the endpoint ACK. The daemon worker
// still owns completion of the operation under the final intent fence.
func (s *WindowsSession) CheckReady(ctx context.Context) (err error) {
	report := func(stage string, err error) error {
		if s.config.ReportFailure != nil {
			s.config.ReportFailure(s.generation, stage, err)
		}
		return err
	}
	b, err := connectivity.OperationFromContext(ctx)
	if err != nil {
		return report("apply_readiness_budget", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return report("apply_readiness_resources", ErrClosed)
	}
	if b != s.operation || !b.Active() {
		return report("apply_readiness_budget", connectivity.ErrConfig)
	}
	stage := "apply_readiness_packets"
	defer func() {
		if err != nil && err != connectivity.ErrSourceChanged {
			err = s.failUpdate(report(stage, err))
		}
	}()
	if err := s.packets.Err(); err != nil {
		return err
	}
	stage = "apply_readiness_address"
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	stage = "apply_readiness_budget"
	if err := connectivity.CheckOperation(ctx); err != nil {
		return err
	}
	stage = s.tunnelReadinessIssue()
	if expected, ok := ctx.Value(sourceEpochKey{}).(uint64); ok {
		if current, _ := s.lease.SourceSignal(); current != expected {
			return report("apply_readiness_source", connectivity.ErrSourceChanged)
		}
	}
	if stage != "" {
		return ErrNoAuthority
	}
	return nil
}

// Caller holds s.mu. Current must be a verified map that passed native
// application, listener, and source fences.
func (s *WindowsSession) tunnelReady() bool {
	return s.tunnelReadinessIssue() == ""
}

func (s *WindowsSession) tunnelReadinessIssue() string {
	if !s.started || s.stopped || s.engine == nil || s.tunnel == nil || s.filters == nil || s.ip == nil || s.packets == nil {
		return "apply_readiness_resources"
	}
	if s.packets.Err() != nil {
		return "apply_readiness_packets"
	}
	if !s.lease.Permissions().NewFlows {
		return "apply_readiness_lease"
	}
	if s.current.Denied() {
		return "apply_readiness_map"
	}
	m, err := s.current.Snapshot()
	if err != nil || m.Revision.Network == 0 || m.Node.AssignedIP == "" {
		return "apply_readiness_map"
	}
	_, err = s.engine.BoundPort()
	if err != nil {
		return "apply_readiness_listener"
	}
	return ""
}

func (s *WindowsSession) ProbePaths(parent context.Context) error {
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if err := s.packets.Err(); err != nil {
		return err
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	if s.supervisor != nil {
		s.checkLANChanges()
	}
	if err := s.engine.ProbeDirect(ctx); err != nil {
		return err
	}
	if s.supervisor == nil || s.policy.resourceCount == 0 {
		return nil
	}
	return s.reconcileResourcePaths(ctx, s.supervisor.transportStatuses(s.current))
}

func (s *WindowsSession) reconcileResourcePaths(ctx context.Context, statuses []*clientv0.PeerTransportStatus) error {
	m, err := s.current.Snapshot()
	if err != nil {
		return err
	}
	sample, err := s.clock.Sample()
	if err != nil {
		return err
	}
	return s.reconcile(m, sample, s.config.ProbeInterval, statuses, s.packets, s.current, func(paths []connectivity.ResourcePath) error {
		p, err := s.profile(ctx, s.current, true)
		if err != nil {
			return err
		}
		defer clear(p.Keys.WireGuardPrivate)
		return s.engine.applyPaths(ctx, m, p.Keys.WireGuardPrivate, paths)
	})
}

func (s *WindowsSession) DirectPaths() ([]DirectPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return nil, ErrClosed
	}
	return s.engine.DirectPaths()
}
