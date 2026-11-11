package connectivity

import (
	"context"
	"sync"
	"time"
)

// OperationBudget measures an accepted command from its original counter receipt.
// It is not map authority. In particular a restart requires a new receipt and
// fresh authorization rather than trusting a persisted counter from another run.
type OperationBudget struct {
	mu            sync.Mutex
	clock         Clock
	profile       string
	generation    uint64
	receipt, last ClockSample
	duration      time.Duration
	failure       error
	complete      bool
}

func (b *OperationBudget) Active() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.complete && b.failure == nil
}

func NewOperationBudget(clock Clock, profile string, generation uint64, receipt ClockSample, duration time.Duration) (*OperationBudget, error) {
	return newCounterBudget(clock, profile, generation, receipt, duration, 30*time.Second)
}

func newCounterBudget(clock Clock, profile string, generation uint64, receipt ClockSample, duration, maximum time.Duration) (*OperationBudget, error) {
	if clock == nil || !identifier(profile) || generation == 0 || !receipt.valid() || duration <= 0 || duration > maximum || receipt.Uptime > time.Duration(1<<63-1)-duration {
		return nil, ErrConfig
	}
	return &OperationBudget{clock: clock, profile: profile, generation: generation, receipt: receipt, last: receipt, duration: duration}, nil
}

// RetryDelay cannot be used as a Connect/restore operation or completed as an
// access outcome. It only measures the approved background-search cooldown.
type RetryDelay struct{ budget *OperationBudget }

func NewRetryDelay(clock Clock, profile string, generation uint64, receipt ClockSample, duration time.Duration) (*RetryDelay, error) {
	if duration < 15*time.Second || duration > 300*time.Second {
		return nil, ErrConfig
	}
	b, err := newCounterBudget(clock, profile, generation, receipt, duration, 300*time.Second)
	if err != nil {
		return nil, err
	}
	return &RetryDelay{budget: b}, nil
}

func (d *RetryDelay) Remaining() (time.Duration, error) {
	if d == nil {
		return 0, ErrConfig
	}
	return d.budget.Remaining()
}

func (b *OperationBudget) remainingLocked() (time.Duration, error) {
	if b.failure != nil {
		return 0, b.failure
	}
	if b.complete {
		return 0, nil
	}
	s, err := b.clock.Sample()
	if err != nil || !s.valid() || s.Epoch != b.last.Epoch || s.Uptime < b.last.UpperUptime || s.Wall.Before(b.last.Wall) {
		b.failure = ErrClock
		return 0, b.failure
	}
	b.last = s
	remaining := b.receipt.Uptime + b.duration - s.UpperUptime
	wallRemaining := b.receipt.Wall.Add(b.duration).Sub(s.Wall)
	if wallRemaining < remaining {
		remaining = wallRemaining
	}
	if remaining <= 0 {
		b.failure = context.DeadlineExceeded
		return 0, b.failure
	}
	return remaining, nil
}

func (b *OperationBudget) Remaining() (time.Duration, error) {
	if b == nil {
		return 0, ErrConfig
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remainingLocked()
}

func (b *OperationBudget) Check() error { _, err := b.Remaining(); return err }

// Complete may be called only after the owner confirms the current generation's
// requested outcome. Until then packet admission also checks this same budget.
// A terminal timeout or clock fault can never be completed or reset.
func (b *OperationBudget) Complete() error {
	if b == nil {
		return ErrConfig
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.remainingLocked(); err != nil {
		return err
	}
	b.complete = true
	return nil
}

func (b *OperationBudget) CheckBinding(profile string, generation uint64) error {
	if b == nil || b.profile != profile || b.generation != generation {
		return ErrConfig
	}
	return b.Check()
}

// MatchesReceipt prevents replacing an accepted command's budget by a later
// receipt of the same generation. Restoration deliberately uses a fresh epoch.
func (b *OperationBudget) MatchesReceipt(receipt ClockSample, duration time.Duration) bool {
	return b != nil && b.receipt.Epoch == receipt.Epoch && b.receipt.Uptime == receipt.Uptime && b.receipt.UpperUptime == receipt.UpperUptime && b.receipt.Wall.Equal(receipt.Wall) && b.duration == duration
}

type operationBudgetKey struct{}

func WithOperationBudget(ctx context.Context, budget *OperationBudget) context.Context {
	return context.WithValue(ctx, operationBudgetKey{}, budget)
}

func OperationFromContext(ctx context.Context) (*OperationBudget, error) {
	if ctx == nil {
		return nil, ErrConfig
	}
	b, ok := ctx.Value(operationBudgetKey{}).(*OperationBudget)
	if !ok || b == nil {
		return nil, ErrConfig
	}
	return b, nil
}

func CheckOperation(ctx context.Context) error {
	if ctx == nil {
		return ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := OperationFromContext(ctx)
	if err != nil {
		return err
	}
	return b.Check()
}
