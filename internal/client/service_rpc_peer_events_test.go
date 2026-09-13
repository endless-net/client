package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCStatusInvalidatesOnlyActiveOwnerPeerCatalog(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	ownerSub, err := m.subscribe(owner, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(ownerSub)
	observerSub, err := m.subscribe(local.Peer{Identity: "observer"}, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(observerSub)
	for _, sub := range []*rpcSubscriber{ownerSub, observerSub} {
		if _, err := sub.next(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	status := &ipc.Status{Metadata: m.Metadata(), ActiveProfileId: profile.ProfileId, ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED}
	if err := m.PublishStatus(status); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*rpcSubscriber{ownerSub, observerSub} {
		event, err := sub.next(t.Context())
		if err != nil || event.GetSnapshot() == nil || event.Sequence != 2 {
			t.Fatal("missing refreshed snapshot", err)
		}
	}
	invalidated, err := ownerSub.next(t.Context())
	if err != nil || invalidated.Sequence != 3 || invalidated.GetInvalidated().GetDomain() != ipc.Domain_DOMAIN_PEERS || invalidated.GetInvalidated().GetProfileId() != profile.ProfileId || invalidated.Metadata.Revision != m.Metadata().Revision {
		t.Fatal("missing scoped peer invalidation", err)
	}
	if len(observerSub.queue) != 0 {
		t.Fatal("observer received private peer invalidation")
	}
	// Rejected stale and unchanged observations must not enqueue extra events.
	err = m.PublishStatus(status)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	status.Metadata = m.Metadata()
	if err := m.PublishStatus(status); err != nil {
		t.Fatal(err)
	}
	if len(ownerSub.queue) != 0 || len(observerSub.queue) != 0 {
		t.Fatal("unchanged or rejected observation invalidated catalog")
	}
}

func TestRPCPeerInvalidationContextBoundaries(t *testing.T) {
	for _, mode := range []string{"no-active-profile", "revoked-owner", "queue-overflow"} {
		t.Run(mode, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			sub, err := m.subscribe(owner, &ipc.BuildIdentity{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			if _, err := sub.next(t.Context()); err != nil {
				t.Fatal(err)
			}
			publish := func() {
				t.Helper()
				status := &ipc.Status{Metadata: m.Metadata(), ActiveProfileId: m.store.Read().RPCState.ActiveProfileID, ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED}
				// Distinct bounded observations exercise queue capacity rather than
				// no-change suppression. No query or network work is performed.
				status.MapRevision = status.Metadata.Revision
				if err := m.PublishStatus(status); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "no-active-profile":
				if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ActiveProfileID = ""; return nil }); err != nil {
					t.Fatal(err)
				}
				publish()
				event, err := sub.next(t.Context())
				if err != nil || event.GetSnapshot() == nil || event.GetSnapshot().Status.ActiveProfileId != "" || len(sub.queue) != 0 {
					t.Fatal("inactive context emitted old profile invalidation", err)
				}
			case "revoked-owner":
				publish() // Keep both private snapshot and invalidation queued.
				if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "replacement"; return nil }); err != nil {
					t.Fatal(err)
				}
				publish()
				event, err := sub.next(t.Context())
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
				if event != nil {
					t.Fatal("revoked owner drained private invalidation")
				}
				fresh, err := m.subscribe(owner, &ipc.BuildIdentity{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer m.unsubscribe(fresh)
				event, err = fresh.next(t.Context())
				if err != nil || event.Sequence != 1 || event.GetSnapshot().Runtime.CallerAccess != ipc.Access_ACCESS_OBSERVER || event.GetSnapshot().Status.ActiveProfileId == profile.ProfileId {
					t.Fatal("reattached observer retained private context", err)
				}
			case "queue-overflow":
				for range rpcEventQueueCount/2 + 1 {
					publish()
				}
				event, err := sub.next(t.Context())
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
				if event != nil {
					t.Fatal("overflow silently delivered partial catalog event sequence")
				}
			}
		})
	}
}
