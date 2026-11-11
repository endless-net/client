package dataplane

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

func TestTransportBudgetCountsPrimaryRetainedAndTransientEngines(t *testing.T) {
	f := newCandidateFixture(t)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := budget.reserve(f.lease, "", enginePrimary)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.release()
	relay, err := budget.reserve(f.lease, "peer", engineRelay)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.release()
	first, err := budget.reserve(f.lease, "peer", engineTransient)
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	if old, err := budget.promote(first); err != nil || old != nil {
		t.Fatal(err)
	}
	second, err := budget.reserve(f.lease, "peer", engineTransient)
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if _, err := budget.reserve(f.lease, "peer", engineTransient); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("legacy primary omitted from 2*N+2 bound", err)
	}
	old, err := budget.promote(second)
	if err != nil || old != first {
		t.Fatal("promotion lost retained receiver", err)
	}
	budget.mu.Lock()
	oldRole, newRole, count := first.role, second.role, len(budget.engines)
	budget.mu.Unlock()
	if oldRole != engineTransient || newRole != engineDirect || count != 4 {
		t.Fatal("promotion stopped counting old receiver")
	}
	first.release()
	first.release()
	third, err := budget.reserve(f.lease, "peer", engineTransient)
	if err != nil {
		t.Fatal("completed close did not free slot", err)
	}
	third.release()
	other, err := connectivity.NewLease(t.Context(), f.clock, "other-profile", 2)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := budget.reserve(other, "peer", engineTransient)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.release()
	if _, err := budget.promote(candidate); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("another owner stole retained peer slot", err)
	}
}

func TestTransportBudgetOnlyTwoTransientAndTwoActiveProbeReceipts(t *testing.T) {
	f := newCandidateFixture(t)
	budget, err := newTransportBudget(4)
	if err != nil {
		t.Fatal(err)
	}
	first, err := budget.reserve(f.lease, "one", engineTransient)
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	second, err := budget.reserve(f.lease, "two", engineTransient)
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if _, err := budget.reserve(f.lease, "three", engineTransient); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("more than two transient engines admitted", err)
	}
	cycle, err := budget.newCycle(t.Context(), f.clock, f.profile.ID, 1, "112233445566778899aabbccddeeff00")
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.cancel()
	a, err := cycle.attempt("one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := cycle.attempt("two")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cycle.attempt("three")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.tryProbe(a); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.tryProbe(b); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.tryProbe(c); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("third active probe admitted", err)
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	next, err := cycle.attempt("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.tryProbe(next); err != nil {
		t.Fatal("suspend-expired slots required a later timer wakeup", err)
	}
	if _, err := budget.tryProbe(a); err == nil {
		t.Fatal("old receipt renewed after capacity was released")
	}
	next.cancel()
	last, err := cycle.attempt("last")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.tryProbe(last); err != nil {
		t.Fatal("cancelled probe retained capacity", err)
	}
}

type heldCloseDevice struct {
	wireGuardDevice
	started, finish chan struct{}
}

func (d *heldCloseDevice) Close() { close(d.started); <-d.finish; d.wireGuardDevice.Close() }

func TestActualEngineSlotIsReleasedOnlyAfterWorkerCloseCompletes(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := budget.newRelay(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	release := func() { unblock.Do(func() { close(finish) }) }
	defer release()
	channel.cipher.mu.Lock()
	original := channel.cipher.device
	channel.cipher.device = &heldCloseDevice{wireGuardDevice: original, started: started, finish: finish}
	channel.cipher.mu.Unlock()
	closed := make(chan struct{})
	go func() { channel.Close(); close(closed) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fixture close did not begin")
	}
	if _, err := budget.reserve(f.lease, "peer", engineRelay); !errors.Is(err, ErrTransportCapacity) {
		t.Fatal("closing engine slot reused prematurely", err)
	}
	select {
	case <-channel.reservation.done:
		t.Fatal("reservation released before real workers stopped")
	default:
	}
	release()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("engine close did not complete")
	}
	replacement, err := budget.newRelay(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	replacement.Close()
}

func TestBudgetedFactoriesReturnFailedReservationsAndCompleteProbePermits(t *testing.T) {
	f := newCandidateFixture(t)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	socket := newCandidateSocketFixture()
	factory := func(uint16) (candidateSocket, error) { return socket, nil }
	if _, err := budget.newAssigned(t.Context(), f.verified, f.lease, "peer", "fixture-profile", make([]byte, 32), 1280, f.clock, func([]byte) error { return nil }, factory); err == nil {
		t.Fatal("invalid key constructed engine")
	}
	budget.mu.Lock()
	count := len(budget.engines)
	budget.mu.Unlock()
	if count != 0 {
		t.Fatal("failed factory retained reservation")
	}
	channel, err := budget.newAssigned(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, func([]byte) error { return nil }, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	cycle, err := budget.newCycle(t.Context(), f.clock, f.profile.ID, 1, "112233445566778899aabbccddeeff00")
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.cancel()
	probe, err := channel.BeginProbe(cycle, cycle.id)
	if err != nil {
		t.Fatal(err)
	}
	budget.mu.Lock()
	count = len(budget.probes)
	budget.mu.Unlock()
	if count != 1 {
		t.Fatal("actual Direct probe omitted global permit")
	}
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	if err := probe.receive(replyControl(probe.request, transport.Echo)); err != nil {
		t.Fatal(err)
	}
	budget.mu.Lock()
	count = len(budget.probes)
	budget.mu.Unlock()
	if count != 0 {
		t.Fatal("completed proof held concurrency slot")
	}
	if _, err := probe.Evidence(); err != nil {
		t.Fatal("permit release invalidated frozen proof", err)
	}
	channel.Close()
}
