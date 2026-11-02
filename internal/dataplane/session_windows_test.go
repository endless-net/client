//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wireguard-go/conn"
)

func TestWindowsSessionVerifiedInitialApplyAndDisconnectUseActualStore(t *testing.T) {
	s, store, _, m, ip, dynamic, guards, tunnel, _ := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	if port, err := s.BoundPort(); err != nil || port != 51820 || !s.lease.Permissions().NewFlows {
		t.Fatal("initial application did not enable confirmed listener", port, err)
	}
	value, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := value.Profiles[cleanupProfile]
	if p.Connection.Journal == nil || len(p.Connection.Journal.Addresses) != 1 || len(p.Connection.Journal.Routes) != 1 || ip.createdAddresses != 1 || ip.createdRoutes != 1 {
		t.Fatal("native effects not journaled")
	}
	for _, f := range p.Connection.Journal.Filters {
		if f.Phase != state.EffectApplied || f.ID == 0 {
			t.Fatal("filter receipt not durably acknowledged")
		}
	}
	// The newer Disconnect intent must survive cleanup of this old generation.
	if err := store.Update(context.Background(), value.Revision, func(value *state.State) error {
		p := value.Profiles[cleanupProfile]
		_, err := beginSessionDisconnect(value, &p)
		value.Profiles[cleanupProfile] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	value, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p = value.Profiles[cleanupProfile]
	if p.Connection.Intent != state.IntentDisconnected || p.Connection.Generation != 4 || p.Connection.Journal != nil || p.Secrets.NodeCredential == "" || len(guards.live)+len(dynamic.live) != 0 || tunnel.closes != 1 || s.lease.Permissions().ExistingFlows {
		t.Fatal("disconnect restored intent, leaked effects or erased registration")
	}
}

func TestWindowsSessionInitialStoreACKLossAlwaysFailsClosed(t *testing.T) {
	for failAt := 1; failAt <= 12; failAt++ {
		s, store, slot, m, _, dynamic, _, _, _ := wiredSessionFixture(t)
		slot.loseACKAt = failAt
		if err := startSessionFixture(t, s, m); !errors.Is(err, state.ErrUncertain) {
			t.Fatal("lost durable ACK not surfaced", failAt, err)
		}
		if s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
			t.Fatal("lost ACK left access enabled", failAt)
		}
		slot.loseACKAt = 0
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal("explicit cleanup failed", failAt, err)
		}
		value, err := store.Load(context.Background())
		if err != nil || value.Profiles[cleanupProfile].Connection.Journal != nil {
			t.Fatal("partial start lost cleanup receipts", failAt, err)
		}
	}
}

func TestWindowsSessionNativeFailureKeepsReceiptForExplicitCleanup(t *testing.T) {
	for _, failure := range []string{"persistent-commit", "dynamic-commit", "address-create", "mtu"} {
		s, store, _, m, ip, dynamic, guards, _, _ := wiredSessionFixture(t)
		switch failure {
		case "persistent-commit":
			guards.fail = "commit"
		case "dynamic-commit":
			dynamic.fail = "commit"
		case "address-create":
			ip.loseCreate = true
		case "mtu":
			s.setMTU = func(context.Context, state.AdapterRecord, int) error { return ErrIPUncertain }
		}
		if err := startSessionFixture(t, s, m); err == nil {
			t.Fatal("native failure claimed local application", failure)
		}
		if s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
			t.Fatal("failed native operation left access", failure)
		}
		value, err := store.Load(context.Background())
		if err != nil || value.Profiles[cleanupProfile].Connection.Journal == nil {
			t.Fatal("failed native start erased journal", failure, err)
		}
		guards.fail, dynamic.fail = "", ""
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal("native partial start could not reconcile", failure, err)
		}
	}
}

func TestWindowsSessionStopCancelsInFlightNativeApplyBeforeActivation(t *testing.T) {
	s, _, _, m, _, dynamic, _, _, _ := wiredSessionFixture(t)
	entered := make(chan struct{})
	s.setMTU = func(ctx context.Context, _ state.AdapterRecord, _ int) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	started := make(chan error, 1)
	go func() { started <- startSessionFixture(t, s, m) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native apply not reached")
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatal("stop failed to cancel native apply", err)
	}
	if s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
		t.Fatal("late apply activated after stop")
	}
}

func TestWindowsSessionFailedDynamicReleaseRetainsAdapterUntilExplicitStop(t *testing.T) {
	s, store, _, m, _, dynamic, _, tunnel, _ := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	dynamic.fail = "close"
	if err := s.Stop(cleanupContext(t)); !errors.Is(err, ErrPolicyUncertain) {
		t.Fatal("failed dynamic release claimed cleanup", err)
	}
	if tunnel.closes != 0 || s.lease.Permissions().ExistingFlows {
		t.Fatal("adapter recycled before old dynamic exceptions were released")
	}
	value, err := store.Load(context.Background())
	if err != nil || value.Profiles[cleanupProfile].Connection.Journal.Adapter.Phase != state.EffectRemoving {
		t.Fatal("failed stop erased cleanup receipt", err)
	}
	dynamic.fail = ""
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsSessionTrustChangeDuringNativeSetupCannotActivate(t *testing.T) {
	s, store, _, m, ip, _, _, _, _ := wiredSessionFixture(t)
	s.setMTU = func(ctx context.Context, _ state.AdapterRecord, _ int) error {
		value, err := store.Load(ctx)
		if err != nil {
			return err
		}
		return store.Update(ctx, value.Revision, func(value *state.State) error {
			p := value.Profiles[cleanupProfile]
			p.Trust = state.ServerTrust{}
			value.Profiles[cleanupProfile] = p
			return nil
		})
	}
	if err := startSessionFixture(t, s, m); !errors.Is(err, state.ErrStale) || ip.createdAddresses+ip.createdRoutes != 0 || s.lease.Permissions().ExistingFlows {
		t.Fatal("old trusted map activated after trust change", err)
	}
}

type sessionMutatingBind struct {
	*inertBind
	mutate func()
}

func (b *sessionMutatingBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actual, err := b.inertBind.Open(port)
	if err == nil {
		b.mutate()
	}
	return fns, actual, err
}

func TestWindowsSessionFinalActivationFenceRejectsIntentChangedDuringBind(t *testing.T) {
	s, store, _, m, _, dynamic, _, _, _ := wiredSessionFixture(t)
	s.makeBind = func() conn.Bind {
		return &sessionMutatingBind{inertBind: &inertBind{}, mutate: func() {
			value, err := store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Update(context.Background(), value.Revision, func(value *state.State) error {
				p := value.Profiles[cleanupProfile]
				_, err := beginSessionDisconnect(value, &p)
				value.Profiles[cleanupProfile] = p
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}}
	}
	if err := startSessionFixture(t, s, m); !errors.Is(err, state.ErrStale) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
		t.Fatal("late bind activated superseded intent", err)
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
}
