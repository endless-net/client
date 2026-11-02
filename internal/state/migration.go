package state

import (
	"context"
	"time"
)

// Upgrade performs the one direct v1 -> v2 data migration before startup Load.
// It neither initializes missing state nor repairs unknown/corrupt state. The
// protected backend uses the same installation/DPAPI binding and atomic Replace;
// a lost Replace ACK remains uncertain and must be resolved by a fresh read.
func (s *Store) Upgrade(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return s.backend.Exclusive(ctx, func(slot Slot) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		value, err := s.decode(slot)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if value.Version == CurrentVersion {
			return value.Validate(s.installation)
		}
		if value.Version != 1 || value.Revision == ^uint64(0) {
			return ErrCorrupt
		}
		for _, profile := range value.Profiles {
			if profile.Connection != nil {
				return ErrCorrupt
			}
		}
		// v1 has no desired connection or network resources. Nil remains an
		// explicit absence of connection intent, never an implicit Connect.
		value.Version = CurrentVersion
		if value.Validate(s.installation) != nil {
			return ErrCorrupt
		}
		value.Revision++
		return s.save(ctx, slot, value)
	})
}
