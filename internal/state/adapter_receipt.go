package state

import "context"

// RecordAdapterCreation prepares exactly one new journal before native create,
// then acknowledges its measured receipt before the TUN can reach an engine.
// An existing prepared journal is never dispatched a second time after restart.
func (s *Store) RecordAdapterCreation(ctx context.Context, profileID string, generation uint64, record AdapterRecord) error {
	if ctx == nil || !validID(record.ID) || record.Name == "" {
		return ErrCorrupt
	}
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
		if c.Intent != IntentConnected || c.Generation != generation {
			return ErrStale
		}
		switch record.Phase {
		case EffectPrepared:
			if record.LUID != 0 || record.Fingerprint != "" {
				return ErrCorrupt
			}
			if c.Journal != nil {
				return ErrStale
			}
			c.Journal = &ResourceJournal{ID: record.ID, Generation: generation, Adapter: &record}
		case EffectApplied:
			j := c.Journal
			if j == nil || j.ID != record.ID || j.Generation != generation || j.Adapter == nil {
				return ErrStale
			}
			old := *j.Adapter
			if old == record {
				return nil
			}
			if old.Phase != EffectPrepared || old.ID != record.ID || old.Name != record.Name || old.LUID != 0 || old.Fingerprint != "" || record.LUID == 0 || !fingerprint(record.Fingerprint) || len(j.Addresses)+len(j.Routes)+len(j.Filters) != 0 {
				return ErrCorrupt
			}
			j.Adapter = &record
		default:
			return ErrCorrupt
		}
		next.Profiles[profileID] = p
		return nil
	})
}
