package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Catalog reads authenticate their source independently. This fingerprint only
// detects when consumers must refetch, including a changed invalid source. Hash
// the actual data rather than trusting the claimed signature payload hash.
func rpcCatalogFingerprint(cfg Config, now time.Time) ([32]byte, error) {
	profile := ""
	if cfg.RPCState != nil {
		profile = cfg.RPCState.ActiveProfileID
	}
	var expired []bool
	if state := cfg.CachedMap; state != nil {
		if state.MapSignature != nil {
			expired = append(expired, !now.Before(state.MapSignature.ExpiresAt))
		}
		if policy := state.Network.ClientPolicy; policy != nil {
			for _, exit := range policy.ExitNodes {
				expired = append(expired, !now.Before(exit.ExpiresAt))
			}
		}
		for _, app := range state.Network.Applications {
			for _, route := range app.Routes {
				expired = append(expired, !now.Before(route.ExpiresAt))
			}
		}
	}
	encoded, err := json.Marshal([]any{profile, cfg.LocalOwnerID, cfg.ActiveAccountID,
		cfg.NodeID, cfg.NetworkID, cfg.MapRevision, cfg.MapGlobalRevision,
		cfg.CachedMap, cfg.MapSigningTrust, expired})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// Independently re-evaluate catalog deadlines, even while control traffic is
// idle. Sharing observedCatalog with status publication prevents duplicate
// invalidations when a network observation and the clock see the same change.
func (m *ClientRPCMutations) publishCatalogClock() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	catalog, err := rpcCatalogFingerprint(cfg, m.now())
	if err != nil {
		return err
	}
	if m.observedCatalog == nil || cfg.RPCState == nil {
		m.observedCatalog = &catalog
		return nil
	}
	if *m.observedCatalog == catalog {
		return nil
	}
	if err := m.store.Update(func(next *Config) error {
		current, err := rpcCatalogFingerprint(*next, m.now())
		if err != nil {
			return err
		}
		if current != catalog || next.RPCState == nil {
			return errRPCNoChange
		}
		next.RPCState.Revision++
		return nil
	}); err != nil {
		if err == errRPCNoChange {
			return nil // Keep the old fingerprint; retry current state next tick.
		}
		return err
	}
	m.observedCatalog = &catalog
	m.publishMutationLocked(nil, ipc.Domain_DOMAIN_PEERS, ipc.Domain_DOMAIN_NETWORKS,
		ipc.Domain_DOMAIN_EXIT_NODE, ipc.Domain_DOMAIN_PREFERENCES,
		ipc.Domain_DOMAIN_MANAGED_SETTINGS, ipc.Domain_DOMAIN_RESOURCES)
	return nil
}

func (m *ClientRPCMutations) startCatalogClock(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	// Capture the source before admission opens, not at the first delayed tick.
	if err := m.publishCatalogClock(); err != nil {
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
				if err := m.publishCatalogClock(); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	return done
}
