package client

import (
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCCatalogChangesInvalidateDespiteIdenticalStatus(t *testing.T) {
	for _, change := range []string{"policy", "tamper", "trust", "removed", "map_expiry", "exit_expiry", "application_expiry", "owner"} {
		t.Run(change, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			now := time.Now()
			m.now = func() time.Time { return now }
			// This is event detection, not map admission. Read handlers retain
			// their independent signature checks, including for invalid sources.
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", ExpiresAt: now.Add(time.Minute)}}}
				cfg.CachedMap.Network.Applications = []api.Application{{ID: "app", Routes: []api.ApplicationRoute{{ExpiresAt: now.Add(2 * time.Minute)}}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			publish := func() error {
				return m.ObserveStatus(func(Config) (*ipc.Status, error) {
					return &ipc.Status{ActiveProfileId: profile.ProfileId, ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED}, nil
				})
			}
			if err := publish(); err != nil {
				t.Fatal(err)
			}
			sub, err := m.subscribe(owner, &ipc.BuildIdentity{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			observer, err := m.subscribe(local.Peer{Identity: "observer"}, &ipc.BuildIdentity{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(observer)
			for _, s := range []*rpcSubscriber{sub, observer} {
				if _, err := s.next(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err := publish(); err != nil {
				t.Fatal(err)
			}
			if len(sub.queue) != 0 || len(observer.queue) != 0 {
				t.Fatal("unchanged probe emitted events")
			}
			if err := m.store.Update(func(cfg *Config) error {
				switch change {
				case "policy":
					behavior := api.ClientLifecycleDisconnect
					cfg.CachedMap.Network.ClientPolicy.Settings = []api.ManagedClientSetting{{Key: api.ClientSettingUIQuit, LifecycleValue: &behavior}}
				case "tamper":
					cfg.CachedMap.Peers[0].Hostname = "changed-with-same-claimed-hash"
				case "trust":
					cfg.MapSigningTrust = nil
				case "removed":
					cfg.CachedMap = nil
				case "map_expiry":
					now = cfg.CachedMap.MapSignature.ExpiresAt
				case "exit_expiry":
					now = now.Add(time.Minute)
				case "application_expiry":
					now = now.Add(2 * time.Minute)
				case "owner":
					cfg.LocalOwnerID = "replacement"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := m.Metadata().Revision
			if err := publish(); err != nil {
				t.Fatal(err)
			}
			if m.Metadata().Revision != before+1 {
				t.Fatal("catalog-only change did not advance revision")
			}
			if change == "owner" {
				event, err := sub.next(t.Context())
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
				if event != nil {
					t.Fatal("old owner received private invalidation")
				}
				return
			}
			seen := map[ipc.Domain]bool{}
			for i := 0; i < 7; i++ {
				event, err := sub.next(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if event.Metadata.Revision != before+1 || event.Sequence != uint64(i+2) {
					t.Fatal("invalidation revision/sequence mismatch")
				}
				if i == 0 {
					if event.GetStatusChanged() == nil {
						t.Fatal("status must precede invalidations")
					}
					continue
				}
				invalid := event.GetInvalidated()
				if invalid == nil || invalid.ProfileId != profile.ProfileId || seen[invalid.Domain] {
					t.Fatal("wrong/duplicate scoped invalidation")
				}
				seen[invalid.Domain] = true
			}
			for _, domain := range []ipc.Domain{ipc.Domain_DOMAIN_PEERS, ipc.Domain_DOMAIN_NETWORKS, ipc.Domain_DOMAIN_EXIT_NODE, ipc.Domain_DOMAIN_PREFERENCES, ipc.Domain_DOMAIN_MANAGED_SETTINGS, ipc.Domain_DOMAIN_RESOURCES} {
				if !seen[domain] {
					t.Fatal("missing domain", domain)
				}
			}
			event, err := observer.next(t.Context())
			if err != nil || event.GetStatusChanged() == nil || event.GetStatusChanged().ActiveProfileId != "" || len(observer.queue) != 0 {
				t.Fatal("observer received private catalog context", err)
			}
			if err := publish(); err != nil {
				t.Fatal(err)
			}
			if m.Metadata().Revision != before+1 || len(sub.queue) != 0 || len(observer.queue) != 0 {
				t.Fatal("same changed catalog was published twice")
			}
		})
	}
}

func TestRPCCatalogRacePreservesLastAcceptedFingerprint(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	if err := m.ObserveStatus(func(Config) (*ipc.Status, error) { return &ipc.Status{}, nil }); err != nil {
		t.Fatal(err)
	}
	before, revision := *m.observedCatalog, m.Metadata().Revision
	err := m.ObserveStatus(func(Config) (*ipc.Status, error) {
		if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Peers[0].Hostname = "concurrent-map"; return nil }); err != nil {
			t.Fatal(err)
		}
		return &ipc.Status{}, nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if *m.observedCatalog != before || m.Metadata().Revision != revision {
		t.Fatal("rejected probe consumed catalog change")
	}
	if err := m.ObserveStatus(func(Config) (*ipc.Status, error) { return &ipc.Status{}, nil }); err != nil {
		t.Fatal(err)
	}
	if *m.observedCatalog == before || m.Metadata().Revision != revision+1 {
		t.Fatal("fresh probe lost the pending catalog invalidation")
	}
}
