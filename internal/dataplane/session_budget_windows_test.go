//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/endless-net/client/internal/state"
)

func TestWindowsSessionSharesCapacityAcrossOwnersBeforeNativeEffects(t *testing.T) {
	first, _, _, firstMap, _, _, _, _, _ := wiredSessionFixture(t)
	second, secondStore, _, secondMap, secondIP, _, _, secondTUN, _ := wiredSessionFixture(t)
	second.resources = first.resources
	if err := startSessionFixture(t, first, firstMap); err != nil {
		t.Fatal(err)
	}
	first.lease.Revoke() // revocation alone does not stop actual engine workers
	if err := startSessionFixture(t, second, secondMap); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("overlapping session bypassed shared primary capacity", err)
	}
	value, err := secondStore.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if secondTUN.closes != 0 || secondIP.createdAddresses != 0 || value.Profiles[cleanupProfile].Connection.Journal != nil {
		t.Fatal("capacity rejection reached native resource creation")
	}
	if err := first.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	third, _, _, thirdMap, _, _, _, _, _ := wiredSessionFixture(t)
	third.resources = first.resources
	if err := startSessionFixture(t, third, thirdMap); err != nil {
		t.Fatal("completed engine close did not return shared capacity", err)
	}
}

func TestWindowsSessionReturnsReservationAfterNativeFailureBeforeEngine(t *testing.T) {
	s, _, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	s.setMTU = func(context.Context, state.AdapterRecord, int) error { return ErrIPUncertain }
	if err := startSessionFixture(t, s, m); !errors.Is(err, ErrIPUncertain) {
		t.Fatal(err)
	}
	s.resources.budget.mu.Lock()
	count := len(s.resources.budget.engines)
	s.resources.budget.mu.Unlock()
	if count != 0 || s.primary != nil {
		t.Fatal("unconstructed engine retained reservation")
	}
}

func TestWindowsSessionRequiresFactoryResourcesWithMatchingLimits(t *testing.T) {
	s, store, _, _, _, _, _, _, clock := wiredSessionFixture(t)
	wrong, err := NewTransportResources(s.config.MaxPeers + 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, resources := range []*TransportResources{nil, {}, wrong} {
		if _, err := NewWindowsSession(t.Context(), store, clock, cleanupProfile, 3, s.config, resources); !errors.Is(err, ErrConfiguration) {
			t.Fatal("missing/mismatched shared resources accepted", err)
		}
	}
}
