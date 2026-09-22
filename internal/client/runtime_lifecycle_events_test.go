package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRuntimeLifecycleSnapshotEventsReattachAndRestart(t *testing.T) {
	m, owner, _ := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	observer := local.Peer{Identity: "different-owner"}
	subscribers := make([]*rpcSubscriber, 0, 2)
	for _, peer := range []local.Peer{owner, observer} {
		sub, err := m.subscribe(peer, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer m.unsubscribe(sub)
		first, err := sub.next(ctx)
		if err != nil || first.Sequence != 1 || first.GetSnapshot() == nil {
			t.Fatal("missing initial snapshot", err)
		}
		subscribers = append(subscribers, sub)
	}
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t, fail: true}
	wakes := 0
	executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() { wakes++ }, func(context.Context) error { return nil }, func(_ context.Context, stopped bool, failure error) error {
		engine.assertLocked()
		return m.ObserveStatus(func(cfg Config) (*ipc.Status, error) {
			phase := ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTING
			if stopped {
				phase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
			}
			status := &ipc.Status{ConnectionPhase: phase, NodeId: cfg.NodeID, Hostname: "private-host"}
			if failure != nil {
				status.Failures = []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "runtime_lifecycle_transition_pending"}}
			}
			return status, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := executor.Close(); err != nil {
			t.Error(err)
		}
	}()
	sequences := []uint64{1, 1}
	assertUpdate := func(phase ipc.ConnectionPhase, failed bool) {
		t.Helper()
		for index, sub := range subscribers {
			for {
				event, err := sub.next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if event.Sequence != sequences[index]+1 {
					t.Fatal("event sequence gap")
				}
				sequences[index] = event.Sequence
				if event.GetOperationChanged() != nil {
					t.Fatal("OS event fabricated an RPC operation")
				}
				status := event.GetStatusChanged()
				if status == nil {
					continue
				}
				if event.Metadata.Revision != m.Metadata().Revision || status.Metadata.Revision != event.Metadata.Revision || status.ConnectionPhase != phase || status.UserDisconnected || status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED {
					t.Fatal("lifecycle event lost revision, phase or durable intent")
				}
				if index == 0 && (len(status.Failures) != 0) != failed {
					t.Fatal("owner failure projection mismatch")
				}
				if index == 1 && (status.NodeId != "" || status.Hostname != "" || len(status.Failures) != 0) {
					t.Fatal("observer received owner-only lifecycle details")
				}
				break
			}
		}
	}
	if err := executor.Handle(ctx, RuntimeSuspend, ""); err == nil {
		t.Fatal("unconfirmed teardown accepted")
	}
	assertUpdate(ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTING, true)
	engine.fail = false
	if err := executor.Handle(ctx, RuntimeSuspend, ""); err != nil {
		t.Fatal(err)
	}
	assertUpdate(ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, false)
	beforeResume := m.Metadata().Revision
	if err := executor.Handle(ctx, RuntimeResume, ""); err != nil || wakes != 1 {
		t.Fatal("resume failed", err)
	}
	if m.Metadata().Revision != beforeResume {
		t.Fatal("unchanged stopped observation created another revision")
	}
	fresh, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(fresh)
	first, err := fresh.next(ctx)
	if err != nil || first.Sequence != 1 || first.GetSnapshot().GetStatus().GetConnectionPhase() != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
		t.Fatal("reattach fabricated a resumed tunnel", err)
	}
	// A new runtime instance must recover durable intent, not restore transient
	// observation as proof that this instance has already applied a tunnel.
	configStores.Delete(m.store.path)
	reopened, err := OpenConfigStore(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewClientRPCMutations(reopened)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := restarted.snapshotAs(owner, nil)
	if err != nil || snapshot.Status.Metadata.InstanceId == m.Metadata().InstanceId || snapshot.Status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || snapshot.Status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED {
		t.Fatal("restart lost intent or reused old runtime observation", err)
	}
}
