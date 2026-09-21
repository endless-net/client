package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func rpcResourceObservationFingerprint(cfg Config, now time.Time, confirmed bool, hosts []string) ([32]byte, error) {
	catalog, err := rpcCatalogFingerprint(cfg, now)
	if err != nil {
		return [32]byte{}, err
	}
	raw, err := json.Marshal([]any{catalog, cfg.ResourcePreferences, confirmed, hosts})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func (s *ClientRPCService) publishResourceClock(ctx context.Context) error {
	if s.ResourceEnforcementProvider == nil && s.ResourceHostProvider == nil {
		return nil
	}
	m := s.mutations
	m.mu.Lock()
	cfg := m.store.Read()
	m.mu.Unlock()
	var observed *ResourceHostObservation
	release := func() {}
	if cfg.RPCState != nil && cfg.RPCState.ActiveProfileID != "" {
		if _, err := compileResourceDenials(cfg, m.now()); err == nil {
			observed, release = s.observeResourceHosts(ctx, cfg)
		}
	}
	defer release()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(m.store.Read())) {
		return nil // A later tick observes the new scope; discard this evidence.
	}
	confirmed := false
	if s.ResourceEnforcementProvider != nil && cfg.RPCState != nil && cfg.RPCState.ActiveProfileID != "" {
		if _, err := compileResourceDenials(cfg, m.now()); err == nil {
			confirmed = s.ResourceEnforcementProvider(cfg, m.now())
		}
	}
	hosts := confirmedResourceHosts(cfg, observed, m.now(), confirmed)
	fingerprint, err := rpcResourceObservationFingerprint(cfg, m.now(), confirmed, hosts)
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
		if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(*next)) {
			return errRPCNoChange
		}
		current, err := rpcResourceObservationFingerprint(*next, m.now(), confirmed, hosts)
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
	if s.ResourceEnforcementProvider == nil && s.ResourceHostProvider == nil {
		return nil
	}
	done := make(chan error, 1)
	if err := s.publishResourceClock(ctx); err != nil {
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
				if err := s.publishResourceClock(ctx); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	return done
}
