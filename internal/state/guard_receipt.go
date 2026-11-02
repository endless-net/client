package state

import "context"

// RecordPreparedGuardIDs acknowledges only already prepared deny effects. The
// WFP owner calls this before committing its transaction; an uncertain store
// response must abort that transaction. This operation never creates a grant,
// changes a phase, retries a CAS, or acknowledges a different intent.
func (s *Store) RecordPreparedGuardIDs(ctx context.Context, profileID, journalID string, generation uint64, records []FilterRecord) error {
	if ctx == nil || len(records) == 0 {
		return ErrCorrupt
	}
	records = append([]FilterRecord(nil), records...)
	value, err := s.Load(ctx)
	if err != nil {
		return err
	}
	return s.Update(ctx, value.Revision, func(next *State) error {
		p, ok := next.Profiles[profileID]
		if !ok || next.ActiveProfileID != profileID || p.Connection == nil {
			return ErrStale
		}
		c := p.Connection
		if c.Intent != IntentConnected || c.Generation != generation || c.Journal == nil || c.Journal.ID != journalID || c.Journal.Generation != generation {
			return ErrStale
		}
		seen := make(map[string]bool, len(records))
		for _, receipt := range records {
			if seen[receipt.Key] || receipt.ID == 0 || receipt.GuardAddress == "" || receipt.Phase != EffectPrepared {
				return ErrCorrupt
			}
			seen[receipt.Key] = true
			found := false
			for i, previous := range c.Journal.Filters {
				if previous.Key != receipt.Key {
					continue
				}
				if previous.Phase == EffectRemoving || previous.ID != 0 && previous.ID != receipt.ID || previous.Phase == EffectApplied && previous.ID == 0 {
					return ErrCorrupt
				}
				expected := previous
				expected.ID, expected.Phase = receipt.ID, EffectPrepared
				if expected != receipt {
					return ErrCorrupt
				}
				c.Journal.Filters[i].ID = receipt.ID
				found = true
				break
			}
			if !found {
				return ErrCorrupt
			}
		}
		next.Profiles[profileID] = p
		return nil
	})
}
