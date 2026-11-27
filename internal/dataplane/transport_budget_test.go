package dataplane

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

func TestKernelPreparationFallbackUsesOneReservationAndCurrentAssignment(t *testing.T) {
	cleanupFailed := errors.New("kernel cleanup incomplete")
	for _, tc := range []struct {
		name        string
		kernelError error
		transition  bool
		revoke      bool
		wantError   error
	}{
		{name: "same profile in Go", kernelError: errKernelUnavailable},
		{name: "cleanup failure stops preparation", kernelError: cleanupFailed, wantError: cleanupFailed},
		{name: "unavailable with cleanup failure", kernelError: errors.Join(errKernelUnavailable, cleanupFailed), wantError: cleanupFailed},
		{name: "source changed during cleanup", kernelError: errKernelUnavailable, transition: true, wantError: ErrNoAuthority},
		{name: "lease revoked during cleanup", kernelError: errKernelUnavailable, revoke: true, wantError: ErrNoAuthority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCandidateFixture(t)
			budget, err := newTransportBudget(1)
			if err != nil {
				t.Fatal(err)
			}
			var digest string
			budget.kernel = func(ctx context.Context, cfg directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
				if len(budget.engines) != 1 || cfg.assignment.lease != f.lease || cfg.assignment.peer.ID != "peer" {
					t.Fatal("incorrect reservation or signed assignment")
				}
				digest = cfg.assignment.offer.Digest
				if tc.revoke {
					f.lease.Revoke()
				}
				if tc.transition {
					epoch := f.lease.BlockSource()
					if err := f.lease.EndSourceTransition(epoch); err != nil {
						t.Fatal(err)
					}
				}
				return nil, tc.kernelError
			}
			opened := 0
			factory := func(uint16) (candidateSocket, error) {
				opened++
				return newCandidateSocketFixture(), nil
			}
			channel, err := budget.newAssigned(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, func([]byte) error { return nil }, factory, budget.kernel)
			if tc.wantError == nil {
				if err != nil {
					t.Fatal(err)
				}
				defer channel.Close()
				if digest == "" || channel.cipher.(*peerCipher).profileDigest != digest || opened != 1 || len(budget.engines) != 1 {
					t.Fatal("fallback changed the profile or allocation")
				}
			} else if !errors.Is(err, tc.wantError) || channel != nil || opened != 0 || len(budget.engines) != 0 {
				t.Fatalf("unexpected fallback or retained allocation: %v", err)
			}
		})
	}
}

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
	if _, err := budget.newAssigned(t.Context(), f.verified, f.lease, "peer", "fixture-profile", make([]byte, 32), 1280, f.clock, func([]byte) error { return nil }, factory, budget.kernel); err == nil {
		t.Fatal("invalid key constructed engine")
	}
	budget.mu.Lock()
	count := len(budget.engines)
	budget.mu.Unlock()
	if count != 0 {
		t.Fatal("failed factory retained reservation")
	}
	channel, err := budget.newAssigned(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, func([]byte) error { return nil }, factory, budget.kernel)
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

type failingCloseSocket struct {
	*candidateSocketFixture
	err error
}

func (s *failingCloseSocket) Close() error {
	_ = s.candidateSocketFixture.Close()
	return s.err
}

func TestKernelPartialListenerCleanupBeforeGoFallback(t *testing.T) {
	closeFailure := errors.New("listener close failed")
	for _, closeErr := range []error{nil, closeFailure} {
		f := newCandidateFixture(t)
		first := &failingCloseSocket{newCandidateSocketFixture(), closeErr}
		var staged *packetPort
		preparer := func(_ context.Context, _ directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
			staged = port
			if _, _, err := channel.bind.Open(0); err != nil {
				return nil, err
			}
			return nil, errKernelUnavailable
		}
		opens := 0
		listen := func(uint16) (candidateSocket, error) {
			opens++
			if opens == 1 {
				return first, nil
			}
			select {
			case <-first.done:
			default:
				t.Error("fallback before previous socket closed")
			}
			select {
			case <-staged.done:
			default:
				t.Error("fallback before previous queue closed")
			}
			return newCandidateSocketFixture(), nil
		}
		channel, err := prepareAssignedCipher(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, func([]byte) error { return nil }, listen, preparer)
		if closeErr != nil {
			if channel != nil || !errors.Is(err, closeErr) || opens != 1 {
				t.Fatalf("retried after uncertain cleanup: opens %d, error %v", opens, err)
			}
			continue
		}
		if err != nil || opens != 2 {
			t.Fatalf("fallback: opens %d, error %v", opens, err)
		}
		channel.Close()
	}
}
