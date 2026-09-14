package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func rpcResourceObservationFingerprint(cfg Config, now time.Time, confirmed bool) ([32]byte, error) {
	catalog, err := rpcCatalogFingerprint(cfg, now)
	if err != nil {
		return [32]byte{}, err
	}
	raw, err := json.Marshal([]any{catalog, cfg.ResourcePreferences, confirmed})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func (s *ClientRPCService) publishResourceClock() error {
	if s.ResourceEnforcementProvider == nil {
		return nil
	}
	m := s.mutations
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	confirmed := false
	if cfg.RPCState != nil && cfg.RPCState.ActiveProfileID != "" {
		if _, err := compileResourceDenials(cfg, m.now()); err == nil {
			confirmed = s.ResourceEnforcementProvider(cfg, m.now())
		}
	}
	fingerprint, err := rpcResourceObservationFingerprint(cfg, m.now(), confirmed)
	if err != nil {
		return err
	}
	if s.observedResourceEnforcement == nil || cfg.RPCState == nil {
		s.observedResourceEnforcement = &fingerprint
		return nil
	}
	if *s.observedResourceEnforcement == fingerprint {
		return nil
	}
	if err := m.store.Update(func(next *Config) error {
		current, err := rpcResourceObservationFingerprint(*next, m.now(), confirmed)
		if err != nil {
			return err
		}
		if current != fingerprint || next.RPCState == nil {
			return errRPCNoChange
		}
		next.RPCState.Revision++
		return nil
	}); err != nil {
		if err == errRPCNoChange {
			return nil
		}
		return err
	}
	s.observedResourceEnforcement = &fingerprint
	m.publishMutationLocked(nil, ipc.Domain_DOMAIN_RESOURCES)
	return nil
}

func (s *ClientRPCService) startResourceClock(ctx context.Context) <-chan error {
	if s.ResourceEnforcementProvider == nil {
		return nil
	}
	done := make(chan error, 1)
	if err := s.publishResourceClock(); err != nil {
		done <- err
		return done
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				if err := s.publishResourceClock(); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	return done
}
