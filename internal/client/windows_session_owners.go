package client

import (
	"errors"
	"strings"
	"sync"
)

// A logoff cannot reliably query a token after the user has left. Bind the
// session while logged on, then consume that exact binding on logoff. These
// records contain only public OS identity, never token handles or credentials.
type windowsSessionOwners struct {
	mu         sync.Mutex
	owners     map[uint32]*windowsSessionOwner
	query      func(uint32) (string, error)
	seeding    bool
	retryAfter uint32
}

type windowsSessionOwner struct {
	identity  string
	retired   bool
	resolving bool
}

const maxWindowsUserSessions = 4096

func newWindowsSessionOwners(query func(uint32) (string, error)) *windowsSessionOwners {
	return &windowsSessionOwners{owners: make(map[uint32]*windowsSessionOwner), query: query, seeding: true}
}

// seed never replaces a newer native notification with an enumeration result.
func (s *windowsSessionOwners) seed(session uint32) error { return s.bind(session, false) }

// logon replaces any prior identity before querying, including on failure.
func (s *windowsSessionOwners) logon(session uint32) error { return s.bind(session, true) }

// Once initial enumeration finishes, release its logoff tombstones and reject
// any late seeding. Normal logoff can then free a slot immediately.
func (s *windowsSessionOwners) finishSeeding() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seeding = false
	for session, owner := range s.owners {
		if owner.retired {
			delete(s.owners, session)
		}
	}
}

func (s *windowsSessionOwners) bind(session uint32, replace bool) error {
	if session == 0 || session == ^uint32(0) || s.query == nil {
		return errors.New("windows session identity unavailable")
	}
	s.mu.Lock()
	if !replace && !s.seeding {
		s.mu.Unlock()
		return nil
	}
	if _, exists := s.owners[session]; exists && !replace {
		s.mu.Unlock()
		return nil
	}
	delete(s.owners, session)
	if len(s.owners) >= maxWindowsUserSessions {
		s.mu.Unlock()
		return errors.New("windows session identity capacity exceeded")
	}
	reservation := &windowsSessionOwner{resolving: true}
	s.owners[session] = reservation
	s.mu.Unlock()
	return s.resolve(session, reservation)
}

func (s *windowsSessionOwners) resolve(session uint32, reservation *windowsSessionOwner) error {
	identity, queryErr := s.query(session)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[session] != reservation {
		return errors.New("windows session identity changed during lookup")
	}
	reservation.resolving = false
	if queryErr != nil || strings.TrimSpace(identity) == "" {
		// Keep an unresolved record so startup enumeration cannot resurrect
		// an older identity after a failed, newer logon notification.
		return errors.New("windows session identity lookup failed")
	}
	reservation.identity = identity
	return nil
}

// Retry at most one unresolved session per tick. Round-robin selection prevents
// a persistently failing low-numbered session from starving other owners.
func (s *windowsSessionOwners) retryUnresolved() (bool, error) {
	s.mu.Lock()
	next, first := ^uint32(0), ^uint32(0)
	for id, owner := range s.owners {
		if owner.retired || owner.resolving || owner.identity != "" {
			continue
		}
		if id < first {
			first = id
		}
		if id > s.retryAfter && id < next {
			next = id
		}
	}
	if next == ^uint32(0) {
		next = first
	}
	if next == ^uint32(0) {
		s.mu.Unlock()
		return false, nil
	}
	reservation := s.owners[next]
	reservation.resolving = true
	s.retryAfter = next
	s.mu.Unlock()
	return true, s.resolve(next, reservation)
}

func (s *windowsSessionOwners) logoff(session uint32) (string, error) {
	if session == 0 || session == ^uint32(0) {
		return "", errors.New("windows user session required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := s.owners[session]
	delete(s.owners, session)
	if s.seeding && len(s.owners) < maxWindowsUserSessions {
		s.owners[session] = &windowsSessionOwner{retired: true}
	}
	if owner == nil || owner.identity == "" {
		return "", errors.New("windows logoff owner unavailable")
	}
	return owner.identity, nil
}
