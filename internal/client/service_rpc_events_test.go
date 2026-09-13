package client

import (
	"context"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCEnrollmentSnapshotActionTracksDurableCallerOperation(t *testing.T) {
	m, peer, req := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	action := &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: "https://control.test/approve"}
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, cfg Config, _ ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		cfg.EnrollmentRequestID = "pending-request"
		return action, save(cfg)
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := m.snapshotAs(peer, nil)
	if err != nil || !proto.Equal(owner.Status.PendingAction, action) || owner.Status.EnrollmentRequestId != "pending-request" {
		t.Fatal("snapshot lost durable approval action", err)
	}
	for _, other := range []local.Peer{{Identity: "uid:2000"}, {Identity: "uid:2000", Administrator: true}} {
		snapshot, err := m.snapshotAs(other, nil)
		if err != nil || snapshot.Status.PendingAction != nil {
			t.Fatal("caller-bound browser action exposed to another caller", err)
		}
	}
	// Delayed observations cannot resurrect a terminal operation's URL.
	m.observedStatus = &ipc.Status{PendingAction: proto.Clone(action).(*ipc.UserAction), EnrollmentRequestId: "stale-request"}
	_, err = m.ReconcileOperation(op.Id, func(cfg *Config, op *ipc.Operation) error {
		cfg.EnrollmentRequestID = ""
		op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		op.UserAction = nil
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(peer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	first, err := sub.next(t.Context())
	if err != nil || first.GetSnapshot().Status.PendingAction != nil || first.GetSnapshot().Status.EnrollmentRequestId != "" {
		t.Fatal("reattachment retained stale approval action", err)
	}
}

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

func TestRPCEventCancellationWinsOverQueuedSnapshot(t *testing.T) {
	m := newRPCStoreTest(t)
	sub, err := m.subscribe(local.Peer{Identity: "uid:1000"}, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Both the queue and ctx.Done are ready: a select alone chooses randomly.
	for range 100 {
		event, err := sub.next(ctx)
		if event != nil || err != context.Canceled {
			t.Fatal("cancelled stream returned a queued event", event, err)
		}
	}
	if len(sub.queue) != 1 {
		t.Fatal("pre-cancelled read consumed the initial snapshot")
	}
}

func TestRPCEventOwnerRevocationClosesPrivateQueue(t *testing.T) {
	for _, sending := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "sending"}[sending], func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			status := &ipc.Status{Metadata: m.Metadata(), ActiveProfileId: profile.ProfileId, AccountId: "private-account"}
			if err := m.PublishStatus(status); err != nil {
				t.Fatal(err)
			}
			aborted := false
			sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, func() { aborted = true })
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			// Keep the private initial snapshot queued; simulate a blocked sender
			// separately to verify the transport-abort hook without blocking I/O.
			sub.mu.Lock()
			sub.sending = sending
			sub.mu.Unlock()
			m.mu.Lock()
			err = m.store.Update(func(cfg *Config) error {
				cfg.LocalOwnerID = "replacement-owner"
				cfg.RPCState.Revision++
				return nil
			})
			if err == nil {
				m.publishMutationLocked(nil)
			}
			m.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if aborted != sending {
				t.Fatal("incorrect blocked-send abort", aborted)
			}
			for range 10 {
				event, err := sub.next(t.Context())
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
				if event != nil {
					t.Fatal("revoked owner received a queued event")
				}
			}
			fresh, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(fresh)
			event, err := fresh.next(t.Context())
			if err != nil || event.Sequence != 1 || event.GetSnapshot().Runtime.CallerAccess != ipc.Access_ACCESS_OBSERVER || event.GetSnapshot().Status.AccountId != "" {
				t.Fatal("reattachment did not start with filtered observer snapshot", err)
			}
		})
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
