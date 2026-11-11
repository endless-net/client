package dataplane

import (
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

// directStability collects independent own-nonce roundtrips from a single
// isolated candidate/cycle. It cannot consume Relay or a caller-supplied timestamp.
type directStability struct {
	mu          sync.Mutex
	channel     *assignedCipher
	cycle       *probeCycle
	assignment  candidateAssignment
	seen        map[[32]byte]bool
	first, last connectivity.ClockSample
	latest      *pathChallenge
	worstRTT    time.Duration
	failure     error
}

type directQualification struct {
	stability *directStability
	latest    *pathChallenge
}

func (*directQualification) String() string   { return "[qualified Direct candidate]" }
func (*directQualification) GoString() string { return "[qualified Direct candidate]" }

func newDirectStability(channel *assignedCipher, cycle *probeCycle) (*directStability, error) {
	if channel == nil || cycle == nil {
		return nil, ErrConfiguration
	}
	if err := cycle.check(); err != nil {
		return nil, err
	}
	channel.bind.mu.Lock()
	a := channel.bind.assignment
	channel.bind.mu.Unlock()
	if a.plan.LocalIntentGeneration != cycle.generation {
		return nil, ErrNoAuthority
	}
	return &directStability{channel: channel, cycle: cycle, assignment: a, seen: make(map[[32]byte]bool)}, nil
}

func (s *directStability) Record(probe *pathChallenge) error {
	if probe == nil || probe.channel != s.channel || probe.attempt.cycle != s.cycle {
		return ErrNoAuthority
	}
	evidence, err := probe.Evidence()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.assignment.sameProbe(probe.assignment) {
		return ErrNoAuthority
	}
	if s.failure != nil {
		return s.failure
	}
	nonce := [32]byte(probe.request.Nonce)
	if s.seen[nonce] {
		return nil
	}
	if len(s.seen) >= transport.MaxProbeBytes/32 {
		return ErrProbeBudget
	}
	if len(s.seen) != 0 && (evidence.At.Epoch != s.last.Epoch || evidence.At.Uptime < s.last.UpperUptime || evidence.At.Wall.Before(s.last.Wall)) {
		s.failure = connectivity.ErrClock
		return s.failure
	}
	if len(s.seen) == 0 {
		s.first = evidence.At
	}
	s.last = evidence.At
	s.latest = probe
	s.seen[nonce] = true
	s.worstRTT = max(s.worstRTT, evidence.RTT)
	return nil
}

func (s *directStability) Qualify() (*directQualification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return nil, s.failure
	}
	if err := s.cycle.check(); err != nil {
		return nil, err
	}
	if len(s.seen) < 3 || s.latest == nil || s.last.Uptime-s.first.UpperUptime < 5*time.Second || s.last.Wall.Sub(s.first.Wall) < 5*time.Second {
		return nil, ErrNoAuthority
	}
	if _, err := s.latest.Evidence(); err != nil {
		return nil, err
	}
	return &directQualification{stability: s, latest: s.latest}, nil
}

// check is an activation fence only. Once selected, application traffic still
// requires current map/lease/ACL, but does not reuse an expired probe as a timer.
// Bilateral prepare/commit remains a separate mandatory supervisor condition.
func (q *directQualification) check(channel *assignedCipher) error {
	if q == nil || q.stability == nil || channel != q.stability.channel {
		return ErrNoAuthority
	}
	current, err := q.stability.Qualify()
	if err != nil {
		return err
	}
	if current.latest != q.latest {
		return ErrNoAuthority
	}
	return nil
}
