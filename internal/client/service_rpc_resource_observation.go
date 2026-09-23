package client

import (
	"context"
	"sort"
	"sync"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Return the lock release even when evidence is unavailable. Callers must not
// hold mutations.mu while acquiring the lock or invoking native commands.
func (s *ClientRPCService) observeResourceHosts(ctx context.Context, cfg Config) (*ResourceHostObservation, func()) {
	noop := func() {}
	if s.ResourceHostProvider == nil || s.ResourceObservationLock == nil || ctx.Err() != nil || !s.ResourceObservationLock.TryLock() {
		return nil, noop
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	observed, err := s.ResourceHostProvider(bounded, clonePersistentConfig(cfg))
	owned := observed
	release := sync.OnceFunc(func() {
		cancel()
		_ = owned.Close()
		s.ResourceObservationLock.Unlock()
	})
	if err != nil || bounded.Err() != nil {
		observed = nil
	}
	return observed, release
}

func confirmedResourceIDs(cfg Config, observed *ResourceHostObservation, now time.Time, enforced bool) []string {
	if !enforced || observed == nil || !observed.Current(cfg, now) || cfg.CachedMap == nil {
		return nil
	}
	var hosts []string
	for _, peer := range cfg.CachedMap.Peers {
		id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)
		if observed.HostConfirmed(id) {
			hosts = append(hosts, id)
		}
	}
	for id := range observed.resources {
		if observed.ResourceConfirmed(id) {
			hosts = append(hosts, id)
		}
	}
	sort.Strings(hosts)
	return hosts
}
