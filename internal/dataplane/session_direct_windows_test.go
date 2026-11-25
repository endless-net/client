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
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestWindowsSoleNodeRequiresVerifiedMapAppliedResourcesAndCurrentLease(t *testing.T) {
	s, _, _, original, _, _, _, _, clock := wiredSessionFixture(t)
	if s.tunnelReady() {
		t.Fatal("empty discovery invented local readiness")
	}
	empty := nextSessionMap(t, s, original, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Peers = []coordinator.Peer{} })
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	operation, _ := sessionFixtureOperation(t, s, parent, false)
	if err := s.Start(operation, empty); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(cleanupContext(t)) }()
	if err := s.CheckReady(operation); err != nil {
		t.Fatal("verified sole-node tunnel did not become ready", err)
	}
	local := s.ConnectionLocal()
	if !local.SoleNodeReady || !local.Known || !local.Applied || local.PeerCount != 0 || local.DirectPaths != 0 || local.RelayPaths != 0 || !local.Permission.NewFlows {
		t.Fatal("local readiness or path counts fabricated", local)
	}
	clock.advance(2 * time.Minute)
	if s.ConnectionLocal().SoleNodeReady {
		t.Fatal("expired map retained local readiness")
	}
	s.mu.Lock()
	s.current = connectivity.Map{}
	s.mu.Unlock()
	if s.ConnectionLocal().SoleNodeReady {
		t.Fatal("unknown map treated as a verified empty map")
	}
}

func TestWindowsReadinessReportsMapSupersededDuringApply(t *testing.T) {
	s, _, _, m, ip, _, _, _, clock := wiredSessionFixture(t)
	var stages []string
	var failures []error
	s.config.ReportFailure = func(_ uint64, operation string, err error) {
		if operation == "apply_readiness_source" && s.stopped {
			t.Error("readiness diagnostic was reported after rollback")
		}
		stages = append(stages, operation)
		failures = append(failures, err)
	}
	parent, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	operation, _ := sessionFixtureOperation(t, s, parent, false)
	operation = WithSourceEpoch(operation, s.BlockSource())
	afterCreate := ip.afterCreate
	ip.afterCreate = func() {
		afterCreate()
		s.BlockSource()
	}
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(cleanupContext(t)) }()
	if err := s.CheckReady(operation); !errors.Is(err, connectivity.ErrSourceChanged) {
		t.Fatal("readiness ignored blocked source", err)
	}
	if s.stopped || s.lease.Permissions().NewFlows || s.lease.Permissions().ExistingFlows {
		t.Fatal("supersession stopped the session or allowed traffic")
	}
	if len(stages) != 2 || stages[0] != "apply_source_superseded" || !errors.Is(failures[0], connectivity.ErrSourceChanged) ||
		stages[1] != "apply_readiness_source" || !errors.Is(failures[1], connectivity.ErrSourceChanged) {
		t.Fatalf("wrong readiness diagnostics: %v %v", stages, failures)
	}
	next := nextSessionMap(t, s, m, clock, func(*coordinator.NetworkMapSnapshot) {})
	epoch, _ := s.lease.SourceSignal()
	operation = WithSourceEpoch(operation, epoch)
	if err := s.Refresh(operation, next); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckReady(operation); err != nil || !s.lease.Permissions().NewFlows {
		t.Fatal("replacement map did not restore readiness", err)
	}
}

func TestWindowsOptimisticDADRevokesReadySessionOnLateDuplicate(t *testing.T) {
	s, _, _, original, ip, _, _, _, clock := wiredSessionFixture(t)
	empty := nextSessionMap(t, s, original, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		snapshot.Peers = []coordinator.Peer{}
	})
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	operation, _ := sessionFixtureOperation(t, s, parent, false)
	if err := s.Start(operation, empty); err != nil {
		t.Fatal(err)
	}
	if len(ip.addrs) != 1 || ip.addrs[0].DadState != winipcfg.DadStatePreferred {
		t.Fatal("Windows did not receive the optimistic DAD request")
	}
	if err := s.CheckReady(operation); err != nil {
		t.Fatal(err)
	}

	ip.addrs[0].DadState = winipcfg.DadStateDuplicate
	if err := s.ProbePaths(parent); !errors.Is(err, ErrIPConflict) {
		t.Fatal("late duplicate did not fail the session", err)
	}
	local := s.ConnectionLocal()
	if local.SoleNodeReady || local.Permission.NewFlows || local.Permission.ExistingFlows {
		t.Fatal("late duplicate left the session ready or permitted traffic", local)
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	if len(ip.addrs) != 0 {
		t.Fatal("cleanup left the duplicate address")
	}
}

func TestWindowsReadyChecksIntentWithoutWaitingForPeer(t *testing.T) {
	s, store, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	operation, b := sessionFixtureOperation(t, s, parent, false)
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckReady(operation); err != nil {
		t.Fatal(err)
	}
	local := s.ConnectionLocal()
	if local.PeerCount != 1 || local.DirectPaths != 0 || local.RelayPaths != 0 || len(local.PeerTransports) != 1 || local.PeerTransports[0].GetSelectedPath() != 0 {
		t.Fatal("ready tunnel must report its peer without inventing a path", local)
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	peer := snapshot.Peers[0]
	if len(local.Peers) != 1 || local.Peers[0].GetId() != peer.ID ||
		local.Peers[0].GetHostname() != peer.Hostname ||
		len(local.Peers[0].GetOverlayAddresses()) != 1 ||
		local.Peers[0].OverlayAddresses[0]+"/32" != peer.AllowedIPs[0] {
		t.Fatal("applied map peer was not projected for local reads", local.Peers)
	}
	if !b.Active() {
		t.Fatal("local readiness completed operation without worker fence")
	}
	value, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), value.Revision, func(next *state.State) error {
		p := next.Profiles[cleanupProfile]
		_, err := beginSessionDisconnect(next, &p)
		next.Profiles[cleanupProfile] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckReady(operation); !errors.Is(err, state.ErrStale) || s.lease.Permissions().ExistingFlows {
		t.Fatal("local readiness ignored newer Disconnect", err)
	}
}
