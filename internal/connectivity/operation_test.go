package connectivity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationBudgetCountsDelayBeforeFirstObservationAndSuspend(t *testing.T) {
	receipt := ClockSample{Wall: time.Now().UTC(), Uptime: time.Hour, UpperUptime: time.Hour + time.Millisecond, Epoch: "operation-fixture"}
	clock := &leaseClock{sample: receipt}
	b, err := NewOperationBudget(clock, "profile", 3, receipt, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(2*time.Second, 20*time.Second)
	remaining, err := b.Remaining()
	if err != nil || remaining != 10*time.Second-time.Millisecond {
		t.Fatal("first observation renewed accepted budget", remaining, err)
	}
	clock.advance(0, 10*time.Second)
	if !errors.Is(b.Check(), context.DeadlineExceeded) || !errors.Is(b.Complete(), context.DeadlineExceeded) {
		t.Fatal("suspend extended operation or late completion reset timeout")
	}
	clock.advance(time.Second, -time.Second)
	if !errors.Is(b.Check(), context.DeadlineExceeded) {
		t.Fatal("terminal timeout reset")
	}
}

func TestOperationBudgetClockFaultCannotBeCompletedOrReplaced(t *testing.T) {
	for _, fault := range []string{"epoch", "wall", "uptime", "sample"} {
		t.Run(fault, func(t *testing.T) {
			receipt := ClockSample{Wall: time.Now().UTC(), Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "operation-fixture"}
			clock := &leaseClock{sample: receipt}
			b, err := NewOperationBudget(clock, "profile", 3, receipt, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			clock.mu.Lock()
			switch fault {
			case "epoch":
				clock.sample.Epoch = "next-runtime"
			case "wall":
				clock.sample.Wall = clock.sample.Wall.Add(-time.Second)
			case "uptime":
				clock.sample.Uptime--
			case "sample":
				clock.err = errors.New("private clock diagnostics")
			}
			clock.mu.Unlock()
			if !errors.Is(b.Check(), ErrClock) {
				t.Fatal("unsafe clock accepted")
			}
			clock.mu.Lock()
			clock.sample = receipt
			clock.err = nil
			clock.mu.Unlock()
			if !errors.Is(b.Complete(), ErrClock) || b.Active() {
				t.Fatal("clock recovery reset terminal operation")
			}
		})
	}
}

func TestOperationBudgetCompletionDoesNotCreateOrExtendAuthority(t *testing.T) {
	for _, complete := range []bool{false, true} {
		f, m, clock, lease, cancel := leaseFixture(t, 300*time.Second)
		if err := lease.Confirm(m); err != nil {
			t.Fatal(err)
		}
		receipt, _ := clock.Sample()
		b, err := NewOperationBudget(clock, f.profile.ID, f.profile.Connection.Generation, receipt, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.GuardOperation(b); err != nil {
			t.Fatal(err)
		}
		if complete {
			if err := b.Complete(); err != nil {
				t.Fatal(err)
			}
		}
		clock.advance(31*time.Second, 31*time.Second)
		if lease.Permit(false) != complete || lease.Permit(true) != complete {
			t.Fatal("packet boundary ignored incomplete operation or revoked completed authority")
		}
		if err := lease.GuardOperation(b); err == nil {
			t.Fatal("operation guard replaced")
		}
		clock.advance(269*time.Second, 269*time.Second)
		if lease.Permit(true) || lease.Permit(false) {
			t.Fatal("completion extended original signed authority")
		}
		cancel()
	}
}

func TestOperationBudgetBindingAndContextDoNotAcceptRenewedReceipt(t *testing.T) {
	receipt := ClockSample{Wall: time.Now().UTC(), Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "operation-fixture"}
	clock := &leaseClock{sample: receipt}
	b, err := NewOperationBudget(clock, "profile", 3, receipt, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !b.MatchesReceipt(receipt, 30*time.Second) || b.CheckBinding("another", 3) == nil || b.CheckBinding("profile", 4) == nil {
		t.Fatal("operation binding ignored")
	}
	later := receipt
	later.Uptime++
	later.UpperUptime++
	later.Wall = later.Wall.Add(time.Nanosecond)
	if b.MatchesReceipt(later, 30*time.Second) {
		t.Fatal("replacement receipt accepted")
	}
	if CheckOperation(context.Background()) == nil {
		t.Fatal("missing operation evidence accepted")
	}
	ctx := WithOperationBudget(context.Background(), b)
	if err := CheckOperation(ctx); err != nil {
		t.Fatal(err)
	}
	clock.advance(30*time.Second, 30*time.Second)
	if !errors.Is(CheckOperation(ctx), context.DeadlineExceeded) {
		t.Fatal("native activation fence ignored counter deadline")
	}
}
