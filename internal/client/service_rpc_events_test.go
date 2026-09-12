package client

import (
	"context"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCSnapshotFirstAndOwnershipRefresh(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	build := &ipc.BuildIdentity{Version: "test"}
	sub, err := m.subscribe(peer, build, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	first, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || first.GetSnapshot().GetRuntime().GetCallerAccess() != ipc.Access_ACCESS_OBSERVER || first.Metadata.Revision != m.Metadata().Revision {
		t.Fatal("invalid initial snapshot")
	}
	req := rpcCreateRequest(t, m)
	req.ControlOrigin = "https://control.test"
	op, err := m.createProfileAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if refresh.Sequence != 2 || refresh.GetSnapshot().GetRuntime().GetCallerAccess() != ipc.Access_ACCESS_OWNER || refresh.Metadata.Revision != op.Metadata.Revision {
		t.Fatal("claim did not refresh role and revision")
	}
	changed, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Sequence != 3 || !proto.Equal(changed.GetOperationChanged(), op) {
		t.Fatal("terminal operation event missing")
	}
	invalidated, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if invalidated.Sequence != 4 || invalidated.GetInvalidated().GetDomain() != ipc.Domain_DOMAIN_PROFILES {
		t.Fatal("profile invalidation missing")
	}
	reconnected, err := m.subscribe(peer, build, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(reconnected)
	fresh, err := reconnected.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Sequence != 1 || fresh.Metadata.Revision != op.Metadata.Revision || len(fresh.GetSnapshot().GetStatus().GetCurrentOperations()) != 0 {
		t.Fatal("reconnect reused sequence or retained terminal operation")
	}
}

func TestRPCObserverSnapshotExcludesPrivateState(t *testing.T) {
	m := newRPCStoreTest(t)
	owner := local.Peer{Identity: "uid:1000"}
	build := &ipc.BuildIdentity{}
	req := rpcCreateRequest(t, m)
	req.ControlOrigin = "https://control.test"
	if _, err := m.createProfileAs(owner, req); err != nil {
		t.Fatal(err)
	}
	_, _, err := m.acceptAs(owner, "/client.v0.ClientService/Connect", &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation}, func(_ *Config, op *ipc.Operation) error { op.ProfileId = "inactive"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	status := &ipc.Status{ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTING, AccountId: "secret-account", NodeId: "secret-node", Hostname: "secret-host",
		Session: &ipc.Session{}, Credential: &ipc.CredentialStatus{}, PendingAction: &ipc.UserAction{BrowserUrl: "https://private.test/approval"}, OverlayAddresses: []string{"10.0.0.1"}}
	status.Metadata = m.Metadata()
	if err := m.PublishStatus(status); err != nil {
		t.Fatal(err)
	}
	observer, err := m.snapshotAs(local.Peer{Identity: "uid:2000"}, build)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := proto.Marshal(observer.Status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "secret") || observer.Status.Session != nil || observer.Status.Credential != nil || observer.Status.PendingAction != nil || len(observer.Status.CurrentOperations) != 0 || len(observer.Status.OverlayAddresses) != 0 {
		t.Fatal("observer received private snapshot data")
	}
	if observer.Status.ConnectionPhase != status.ConnectionPhase {
		t.Fatal("observer lost public connection phase")
	}
	ownerSnapshot, err := m.snapshotAs(owner, build)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerSnapshot.Status.CurrentOperations) != 1 || ownerSnapshot.Status.CurrentOperations[0].Kind != ipc.OperationKind_OPERATION_KIND_CONNECT || ownerSnapshot.Status.AccountId != "secret-account" {
		t.Fatal("owner snapshot lost current operation/state")
	}
	status.AccountId = "mutated-input"
	again, _ := m.snapshotAs(owner, build)
	if again.Status.AccountId != "secret-account" {
		t.Fatal("provider retained mutable status alias")
	}
	previous := m.Metadata().Revision
	identical := proto.Clone(m.observedStatus).(*ipc.Status)
	identical.Metadata = m.Metadata()
	if err := m.PublishStatus(identical); err != nil {
		t.Fatal(err)
	}
	if m.Metadata().Revision != previous {
		t.Fatal("identical observation invalidated snapshots")
	}
}

func TestRPCEventQueueNeverSilentlyDrops(t *testing.T) {
	for _, large := range []bool{false, true} {
		s := &rpcSubscriber{queue: make(chan *ipc.WatchEventsResponse, rpcEventQueueCount), done: make(chan struct{})}
		event := &ipc.WatchEventsResponse{Event: &ipc.WatchEventsResponse_StatusChanged{StatusChanged: &ipc.Status{}}}
		if large {
			event.GetStatusChanged().Hostname = strings.Repeat("x", 3<<20)
		}
		limit := rpcEventQueueCount
		if large {
			limit = 2
		}
		for i := 0; i < limit; i++ {
			s.enqueue(event)
		}
		if s.closed {
			t.Fatal("queue closed below limit")
		}
		s.enqueue(event)
		_, err := s.next(t.Context())
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		if s.sequence != uint64(limit) {
			t.Fatal("dropped event consumed a sequence")
		}
	}
}

func TestRPCUnsubscribeAndCancellation(t *testing.T) {
	m := newRPCStoreTest(t)
	sub, err := m.subscribe(local.Peer{Identity: "uid:1000"}, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sub.next(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sub.next(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	m.unsubscribe(sub)
	if len(m.subscribers) != 0 {
		t.Fatal("subscriber leaked")
	}
}

func TestRPCObservationRejectsConcurrentConfigChange(t *testing.T) {
	m := newRPCStoreTest(t)
	err := m.ObserveStatus(func(Config) (*ipc.Status, error) {
		if err := m.store.Update(func(cfg *Config) error { cfg.NetworkID = "changed-during-probe"; return nil }); err != nil {
			t.Fatal(err)
		}
		return &ipc.Status{NodeId: "stale-node"}, nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if m.observedStatus != nil {
		t.Fatal("stale observation became visible")
	}
	err = m.ObserveStatus(func(cfg Config) (*ipc.Status, error) { return &ipc.Status{NodeId: cfg.NodeID}, nil })
	if err != nil {
		t.Fatal(err)
	}
	err = m.ObserveStatus(func(cfg Config) (*ipc.Status, error) {
		if err := m.store.Update(func(cfg *Config) error { cfg.NetworkID = "changed-again"; return nil }); err != nil {
			t.Fatal(err)
		}
		return &ipc.Status{NodeId: cfg.NodeID}, nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
}
