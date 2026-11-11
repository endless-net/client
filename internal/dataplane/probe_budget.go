package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

var ErrProbeBudget = errors.New("transport probe budget exhausted")

// probeCycle is one candidate's frozen cycle receipt, independent of the accepted
// Connect budget. Its parent is the supervisor's shared search-cycle context.
// New signatures or repeated control messages cannot renew either deadline.
type probeCycle struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	clock      connectivity.Clock
	timer      *connectivity.OperationBudget
	profile    string
	generation uint64
	id         string
	wire       *probeWireBudget
	failure    error
	attempts   map[string]*probeAttempt
	budget     *transportBudget
	parent     *probeCycle
}

// One profile's traffic allowance within a frozen peer search. Candidate IDs
// and crypto objects may change; neither can renew this byte allowance.
type probeWireBudget struct {
	mu   sync.Mutex
	used int
}

func (b *probeWireBudget) consumed() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

type probeAttempt struct {
	mu             sync.Mutex
	failure        error
	cycle          *probeCycle
	ctx            context.Context
	cancel         context.CancelFunc
	timer          *connectivity.OperationBudget
	id             string
	owner          *candidateBind
	assignment     *candidateAssignment
	preludeStarted bool
	preludeError   error
	sequence       int
}

func (p *probeAttempt) abort(err error) {
	if p == nil || err == nil {
		return
	}
	p.mu.Lock()
	if p.failure == nil {
		p.failure = err
	}
	p.mu.Unlock()
	p.cancel()
}

func newProbeCycle(parent context.Context, clock connectivity.Clock, profile string, generation uint64, id string) (*probeCycle, error) {
	if parent == nil || parent.Err() != nil || clock == nil || id == "" || len(id) > 128 {
		return nil, ErrConfiguration
	}
	receipt, err := clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	timer, err := connectivity.NewOperationBudget(clock, profile, generation, receipt, 30*time.Second)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	return &probeCycle{ctx: ctx, cancel: cancel, clock: clock, timer: timer, profile: profile, generation: generation, id: id, wire: &probeWireBudget{}, attempts: map[string]*probeAttempt{}}, nil
}

func (c *probeCycle) check() error {
	if c == nil {
		return ErrNoAuthority
	}
	if c.parent != nil {
		if err := c.parent.check(); err != nil {
			c.cancel()
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return c.failure
	}
	if err := c.ctx.Err(); err != nil {
		c.failure = err
		return err
	}
	if err := c.timer.Check(); err != nil {
		c.failure = err
		c.cancel()
		return err
	}
	return nil
}

func (c *probeCycle) child(id string) (*probeCycle, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	child, err := newProbeCycle(c.ctx, c.clock, c.profile, c.generation, id)
	if err != nil {
		return nil, err
	}
	child.parent, child.budget = c, c.budget
	return child, nil
}

func (c *probeCycle) remaining() (time.Duration, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	remaining, err := c.timer.Remaining()
	if err != nil {
		return 0, err
	}
	if c.parent != nil {
		parent, err := c.parent.remaining()
		if err != nil {
			return 0, err
		}
		remaining = min(remaining, parent)
	}
	return remaining, nil
}

func (c *probeCycle) charge(size int) error {
	if size < 0 || size > transport.MaxProbeBytes {
		return ErrProbeBudget
	}
	if err := c.check(); err != nil {
		return err
	}
	c.wire.mu.Lock()
	defer c.wire.mu.Unlock()
	if size > transport.MaxProbeBytes-c.wire.used {
		return ErrProbeBudget
	}
	c.wire.used += size
	return nil
}

func (c *probeCycle) attempt(id string) (*probeAttempt, error) {
	if id == "" || len(id) > 128 {
		return nil, ErrConfiguration
	}
	if err := c.check(); err != nil {
		return nil, err
	}
	remaining, err := c.remaining()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous := c.attempts[id]; previous != nil {
		return previous, nil
	}
	// The byte budget also bounds unique nonce/attempt bookkeeping. A repeated
	// nonce retains its original deadline, even when it has already expired.
	if len(c.attempts) >= transport.MaxProbeBytes/32 {
		return nil, ErrProbeBudget
	}
	duration := min(3*time.Second, remaining)
	receipt, err := c.clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	timer, err := connectivity.NewOperationBudget(c.clock, c.profile, c.generation, receipt, duration)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(c.ctx, duration)
	attempt := &probeAttempt{cycle: c, ctx: ctx, cancel: cancel, timer: timer, id: id, sequence: len(c.attempts) + 1}
	c.attempts[id] = attempt
	return attempt, nil
}

func (p *probeAttempt) check() error {
	if p == nil {
		return ErrNoAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failure != nil {
		return p.failure
	}
	if err := p.cycle.check(); err != nil {
		p.failure = err
		return err
	}
	if err := p.ctx.Err(); err != nil {
		p.failure = err
		return err
	}
	if err := p.timer.Check(); err != nil {
		p.failure = err
		p.cancel()
		return err
	}
	return nil
}
