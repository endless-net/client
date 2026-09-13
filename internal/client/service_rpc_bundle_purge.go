package client

import (
	"strings"

	"google.golang.org/protobuf/proto"
)

// Logical revocation is enforced by every read. This bounded periodic sweep
// additionally removes bytes from durable storage, including crash orphans.
func (s *ClientRPCService) purgeBundles() error {
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	cfg := s.mutations.store.Read()
	s.bundleStore.mu.Lock()
	defer s.bundleStore.mu.Unlock()
	remaining := make(map[string]clientRPCBundleRecord, len(s.bundleStore.items))
	total := 0
	for id, item := range s.bundleStore.items {
		keep := false
		if s.bundleStore.timeNow().Before(item.metadata.ExpiresAt.AsTime()) && strings.EqualFold(cfg.LocalOwnerID, item.installationOwner) && cfg.RPCState != nil {
			if plan, pending := cfg.RPCState.Bundles[id]; pending {
				keep = strings.EqualFold(plan.Owner, item.owner) && plan.ProfileID == item.profile && bundlePlanAllowed(cfg, plan)
			} else {
				profile, metadata, err := publishedRPCBundle(cfg, item.owner, id)
				keep = err == nil && profile == item.profile && proto.Equal(metadata, item.metadata)
			}
		}
		if keep {
			remaining[id] = item
			total += len(item.data)
		}
	}
	if len(remaining) == len(s.bundleStore.items) && !s.bundleStore.dirty {
		return nil
	}
	if s.bundleStore.persist != nil {
		if err := s.bundleStore.persist(remaining); err != nil {
			return err
		}
	}
	s.bundleStore.items, s.bundleStore.bytes = remaining, total
	s.bundleStore.dirty = false
	return nil
}
