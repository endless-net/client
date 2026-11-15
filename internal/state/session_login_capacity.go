package state

import (
	"encoding/json"
	"time"
)

// CheckSessionLoginCapacity reserves known journal/send/failure metadata before
// dispatch. Network payloads remain bounded by the adapter and must still pass
// the atomic save limit; an oversized result is a local failure, never eviction.
func (s *Store) CheckSessionLoginCapacity(value State, profile string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return ErrCorrupt
	}
	defer clear(raw)
	var shadow State
	if json.Unmarshal(raw, &shadow) != nil {
		return ErrCorrupt
	}
	p, ok := shadow.Profiles[profile]
	if !ok || p.SessionLogin == nil {
		return ErrCorrupt
	}
	l := p.SessionLogin
	// Sizing only: these synthetic timestamps are never validated or persisted.
	longTime := time.Date(l.CreatedAt.Year(), l.CreatedAt.Month(), l.CreatedAt.Day(), 23, 59, 59, 999999999, time.UTC)
	l.ObservedAt, l.SentAt, l.InFlightAt, l.RecoveryUntil, l.NextPollAt = longTime, longTime, longTime, longTime, longTime
	l.Phase = SessionLoginPending
	for reason := range sessionLoginReasons {
		if len(reason) > len(l.Reason) {
			l.Reason = reason
		}
	}
	shadow.Profiles[profile] = p
	reserved, err := json.Marshal(shadow)
	if err != nil {
		return ErrCorrupt
	}
	defer clear(reserved)
	if int64(len(reserved)) > s.maxPlain {
		return ErrCapacity
	}
	return nil
}
