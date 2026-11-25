package dataplane

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
)

func TestLinuxSignedMapStartsAndRevokesNativeSession(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	fixture := newCandidateFixture(t)
	fixture.snapshot.Peers = nil
	fixture.verify(t, nil)
	store, _ := hintStoreFixture(t, fixture)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(directory)
	resources, err := NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := fixture.clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	budget, err := connectivity.NewOperationBudget(fixture.clock, fixture.profile.ID, 1, sample, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewLinuxSession(t.Context(), store, ledger, fixture.clock, fixture.profile.ID, 1, LinuxSessionConfig{
		MTU: 1420, MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, ListenPort: 51820,
		ProbeInterval: time.Second, RetryInterval: time.Second,
		Relay: RelayConfig{DialTimeout: time.Second, IOTimeout: 3 * time.Second, HeartbeatInterval: time.Second, Now: func() time.Time { return sample.Wall }},
	}, resources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := session.Stop(cleanup); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = connectivity.WithOperationBudget(ctx, budget)
	if err := session.Start(ctx, fixture.verified); err != nil {
		t.Fatal(err)
	}
	if err := session.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	if local := session.ConnectionLocal(); !local.Applied || !local.SoleNodeReady || local.Address != "100.70.0.1" || !local.Permission.NewFlows {
		t.Fatalf("unexpected Linux readiness: %#v", local)
	}
	fixture.clock.advance(time.Minute, time.Minute)
	if local := session.ConnectionLocal(); local.Permission.NewFlows || local.Permission.ExistingFlows {
		t.Fatal("expired map retained packet authority")
	}
	session.RevokeSource()
	if local := session.ConnectionLocal(); local.Permission.NewFlows || local.Permission.ExistingFlows {
		t.Fatal("revoked session retained packet authority")
	}
}
