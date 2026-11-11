package connectivity

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/state"
)

var ErrClock = errors.New("connectivity clock unsafe")
var ErrExpired = errors.New("connectivity authority expired")
var ErrSourceChanged = errors.New("newer connectivity source state pending")

type Permission struct {
	NewFlows, ExistingFlows bool
	Revoked, ClockUnsafe    bool
	ExpiresAt               time.Time
}

// Lease must be consulted at the packet boundary, not only by a background
// timer. Thus a scheduler delay/resume cannot let an expired flow send first.
// Flow tracking and native deny/teardown remain separate dataplane obligations.
type Lease struct {
	mu                             sync.Mutex
	ctx                            context.Context
	clock                          Clock
	profileID                      string
	generation                     uint64
	last                           ClockSample
	cursor                         *state.MapCursor
	current                        Map
	deadline                       time.Duration
	expires                        time.Time
	active, online, revoked, fault bool
	paused                         bool
	operation                      *OperationBudget
	sourceEpoch                    uint64
	sourceBlocked                  bool
	sourceChanged                  chan struct{}
}

func (*Lease) String() string   { return "[private connectivity lease]" }
func (*Lease) GoString() string { return "[private connectivity lease]" }

func NewLease(ctx context.Context, clock Clock, profileID string, generation uint64) (*Lease, error) {
	if ctx == nil || ctx.Err() != nil || clock == nil || !identifier(profileID) || generation == 0 {
		return nil, ErrConfig
	}
	sample, err := clock.Sample()
	if err != nil || !sample.valid() {
		return nil, ErrClock
	}
	return &Lease{ctx: ctx, clock: clock, profileID: profileID, generation: generation, last: sample, sourceChanged: make(chan struct{})}, nil
}

func (l *Lease) sample() (ClockSample, error) {
	if l.fault {
		return ClockSample{}, ErrClock
	}
	s, err := l.clock.Sample()
	if err != nil || !s.valid() || s.Epoch != l.last.Epoch || s.Uptime < l.last.UpperUptime || s.Wall.Before(l.last.Wall) {
		l.fault = true
		l.active = false
		l.online = false
		return ClockSample{}, ErrClock
	}
	l.last = s
	return s, nil
}

// Confirm accepts only the immutable verifier result for this intent. Reusing
// that result anchors to its original receipt sample, never to this call time.
func (l *Lease) Confirm(m Map) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ctx.Err() != nil {
		l.active = false
		l.online = false
		return l.ctx.Err()
	}
	if m.snapshot.MapSignature == nil || m.profileID != l.profileID || m.generation != l.generation || !m.verified.valid() {
		return ErrMap
	}
	if l.revoked {
		return ErrRevoked
	}
	cursor := m.Cursor()
	if l.cursor != nil {
		if cursor.Revision.Network < l.cursor.Revision.Network || cursor.IssuedAt.Before(l.cursor.IssuedAt) {
			return ErrStale
		}
		if cursor.PayloadHash == l.cursor.PayloadHash {
			if l.revoked {
				return ErrRevoked
			}
			return ErrReplay
		}
		if cursor.Revision == l.cursor.Revision && !cursor.IssuedAt.After(l.cursor.IssuedAt) {
			return ErrStale
		}
	}
	if m.Denied() {
		l.cursor = &cursor
		l.current = m
		l.revoked = true
		l.active = false
		l.online = false
		return ErrRevoked
	}
	now, err := l.sample()
	if err != nil {
		return err
	}
	if m.verified.Epoch != now.Epoch || m.verified.Uptime > now.UpperUptime || m.verified.Wall.After(now.Wall) {
		l.fault = true
		l.active = false
		l.online = false
		return ErrClock
	}
	remaining := m.ExpiresAt().Sub(m.verified.Wall)
	if remaining > 300*time.Second {
		remaining = 300 * time.Second
	}
	if remaining <= 0 || m.verified.Uptime > time.Duration(1<<63-1)-remaining {
		return ErrExpired
	}
	deadline := m.verified.Uptime + remaining
	if now.UpperUptime >= deadline || !now.Wall.Before(m.ExpiresAt()) {
		l.active = false
		l.online = false
		return ErrExpired
	}
	l.cursor = &cursor
	l.current = m
	l.deadline = deadline
	l.expires = m.ExpiresAt()
	l.active = true
	l.online = true
	l.revoked = false
	return nil
}

// Unavailable prevents new flows immediately. It never creates a new grace
// window: existing flows retain only the original signed/receipt deadline.
func (l *Lease) Unavailable() { l.mu.Lock(); l.online = false; l.mu.Unlock() }

func (l *Lease) Revoke() {
	l.mu.Lock()
	l.revoked = true
	l.active = false
	l.online = false
	l.mu.Unlock()
}

// PausePackets quiesces native policy transitions. Resuming never creates or
// renews a grant: original cursor/deadline, offline mode and terminal fences stay.
func (l *Lease) PausePackets(paused bool) {
	l.mu.Lock()
	if l.paused != paused {
		close(l.sourceChanged)
		l.sourceChanged = make(chan struct{})
	}
	l.paused = paused
	l.mu.Unlock()
}

// BlockSource closes the packet gate independently of native apply's pause.
// I/O callbacks must not wait for an in-flight native mutation to do this.
func (l *Lease) BlockSource() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sourceEpoch == ^uint64(0) {
		l.fault = true
		l.active = false
	} else {
		l.sourceEpoch++
	}
	l.sourceBlocked = true
	l.online = false
	close(l.sourceChanged)
	l.sourceChanged = make(chan struct{})
	return l.sourceEpoch
}
func (l *Lease) SourceSignal() (uint64, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sourceEpoch, l.sourceChanged
}
func (l *Lease) EndSourceTransition(epoch uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.revoked || l.ctx.Err() != nil {
		return ErrRevoked
	}
	if l.fault {
		return ErrClock
	}
	if l.sourceEpoch != epoch {
		return ErrSourceChanged
	}
	l.sourceBlocked = false
	close(l.sourceChanged)
	l.sourceChanged = make(chan struct{})
	return nil
}

func (l *Lease) GuardOperation(b *OperationBudget) error {
	if err := b.CheckBinding(l.profileID, l.generation); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.operation != nil || !b.Active() || b.receipt.Epoch != l.last.Epoch {
		return ErrConfig
	}
	l.operation = b
	return nil
}

func (l *Lease) Permissions() Permission {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ctx.Err() != nil {
		l.active = false
		l.online = false
	}
	if l.operation != nil {
		if err := l.operation.Check(); err != nil {
			l.active = false
			l.online = false
			if errors.Is(err, ErrClock) {
				l.fault = true
			}
		}
	}
	if !l.fault {
		if now, err := l.sample(); err == nil && l.active && (now.UpperUptime >= l.deadline || !now.Wall.Before(l.expires)) {
			l.active = false
			l.online = false
		}
	}
	return Permission{NewFlows: l.active && l.online && !l.revoked && !l.fault && !l.paused && !l.sourceBlocked, ExistingFlows: l.active && !l.revoked && !l.fault && !l.paused && !l.sourceBlocked, Revoked: l.revoked, ClockUnsafe: l.fault, ExpiresAt: l.expires}
}

func (l *Lease) Permit(existing bool) bool {
	p := l.Permissions()
	if existing {
		return p.ExistingFlows
	}
	return p.NewFlows
}

// MatchesMap binds an adapter to this intent's most recently confirmed map.
// It is not a traffic grant; every I/O still checks Permissions.
func (l *Lease) MatchesMap(m Map) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cursor != nil && l.profileID == m.profileID && l.generation == m.generation && *l.cursor == m.Cursor()
}

// CurrentMap returns only the last immutable verifier result accepted by this
// intent. It grants no traffic: expiry, revocation and source/native fences
// still require Permissions. Snapshot remains detached from the sealed result.
func (l *Lease) CurrentMap() Map {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current
}
