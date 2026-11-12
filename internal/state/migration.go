package state

import (
	"context"
	"time"
)

// Upgrade directly preserves v1–v6 data in v7 before startup Load.
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
		if (value.Version < 1 || value.Version > 6) || value.Revision == ^uint64(0) {
			return ErrCorrupt
		}
		if value.Version < 4 && (len(value.TransportHints) != 0 || !value.HintObservedAt.IsZero()) {
			return ErrCorrupt
		}
		if value.Version < 5 && len(value.BootstrapCheckpoints) != 0 {
			return ErrCorrupt
		}
		for _, profile := range value.Profiles {
			if value.Version < 6 && profile.DefaultTarget != nil {
				return ErrCorrupt
			}
			if profile.ActiveSession != nil || profile.ActiveNodeCredential != nil || profile.SessionRefresh != nil || profile.NodeRenewal != nil {
				return ErrCorrupt
			}
			if value.Version == 1 && profile.Connection != nil || value.Version < 3 && len(profile.RemoteRevocations) != 0 {
				return ErrCorrupt
			}
		}
		// Preserve v2 connections, v3 revocations, v4 transport hints and all keys.
		// Migration never invents a success hint or a remote command.
		value.Version = CurrentVersion
		if value.Validate(s.installation) != nil {
			return ErrCorrupt
		}
		value.Revision++
		return s.save(ctx, slot, value)
	})
}
