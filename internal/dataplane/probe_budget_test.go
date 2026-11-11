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

type probeClock struct {
	mu     sync.Mutex
	sample connectivity.ClockSample
	err    error
}

func (c *probeClock) Sample() (connectivity.ClockSample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sample, c.err
}
func (c *probeClock) advance(wall, uptime time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sample.Wall = c.sample.Wall.Add(wall)
	c.sample.Uptime += uptime
	c.sample.UpperUptime += uptime
}
func probeBudgetFixture(t *testing.T) (*probeCycle, *probeClock) {
	t.Helper()
	clock := &probeClock{sample: connectivity.ClockSample{Wall: time.Now().UTC(), Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "probe-unit"}}
	cycle, err := newProbeCycle(t.Context(), clock, "profile", 1, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	return cycle, clock
}

func TestProbeBudgetDoesNotRenewRepeatedNonceOrCycle(t *testing.T) {
	cycle, clock := probeBudgetFixture(t)
	first, err := cycle.attempt("nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(3*time.Second, 3*time.Second)
	repeated, err := cycle.attempt("nonce-1")
	if err != nil || repeated != first || !errors.Is(repeated.check(), context.DeadlineExceeded) {
		t.Fatal("repeated nonce renewed its original attempt", err)
	}
	next, err := cycle.attempt("nonce-2")
	if err != nil || next.check() != nil {
		t.Fatal("fresh attempt inside original cycle rejected", err)
	}
	clock.advance(26*time.Second, 26*time.Second)
	last, err := cycle.attempt("nonce-3")
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second, time.Second)
	if !errors.Is(last.check(), context.DeadlineExceeded) || !errors.Is(cycle.charge(1), context.DeadlineExceeded) {
		t.Fatal("last attempt extended original 30 second cycle")
	}
	if _, err := cycle.attempt("nonce-4"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired cycle restarted", err)
	}
}

func TestProbeByteBudgetSharedAcrossAttemptsAndConcurrentWriters(t *testing.T) {
	cycle, _ := probeBudgetFixture(t)
	if _, err := cycle.attempt("handshake"); err != nil {
		t.Fatal(err)
	}
	if err := cycle.charge(transport.MaxProbeBytes - 64); err != nil {
		t.Fatal(err)
	}
	if _, err := cycle.attempt("echo"); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 128)
	for range 128 {
		workers.Go(func() { results <- cycle.charge(1) })
	}
	workers.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrProbeBudget) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 64 || rejected != 64 {
		t.Fatal("concurrent sends exceeded frozen candidate byte budget", accepted, rejected)
	}
	if err := cycle.charge(-1); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("negative charge accepted")
	}
	if err := cycle.charge(transport.MaxProbeBytes + 1); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("oversized charge accepted")
	}
}

func TestProbeBudgetSuspendClockFaultAndCancellation(t *testing.T) {
	t.Run("suspend", func(t *testing.T) {
		cycle, clock := probeBudgetFixture(t)
		attempt, err := cycle.attempt("nonce")
		if err != nil {
			t.Fatal(err)
		}
		clock.advance(0, 4*time.Second)
		if !errors.Is(attempt.check(), context.DeadlineExceeded) {
			t.Fatal("suspend renewed attempt")
		}
		clock.advance(0, 26*time.Second)
		if !errors.Is(cycle.check(), context.DeadlineExceeded) {
			t.Fatal("suspend renewed cycle")
		}
	})
	t.Run("clock", func(t *testing.T) {
		cycle, clock := probeBudgetFixture(t)
		clock.advance(-time.Second, 0)
		if !errors.Is(cycle.check(), connectivity.ErrClock) {
			t.Fatal("clock rollback accepted")
		}
		clock.advance(time.Second, 0)
		if cycle.check() == nil {
			t.Fatal("clock recovery reopened failed cycle")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		cycle, _ := probeBudgetFixture(t)
		attempt, err := cycle.attempt("nonce")
		if err != nil {
			t.Fatal(err)
		}
		cycle.cancel()
		if !errors.Is(attempt.check(), context.Canceled) {
			t.Fatal("cancellation lost at attempt boundary")
		}
	})
}
