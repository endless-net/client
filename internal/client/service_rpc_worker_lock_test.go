package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestPreferenceWorkerLockCancellationPreservesRetry(t *testing.T) {
	for _, contended := range []string{"profile", "effect"} {
		t.Run(contended, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
			if err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			starts, stops := 0, 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { starts++; return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}}
			lock := driver.Lock
			if contended == "profile" {
				lock = &m.profileWorker
			}
			lock.Lock()
			var unlock sync.Once
			release := func() { unlock.Do(lock.Unlock) }
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			entered := make(chan struct{})
			go func() { close(entered); done <- m.ReconcileNetworkPreferences(ctx, driver) }()
			<-entered
			if contended == "effect" {
				// Confirm partial acquisition before cancelling the second wait.
				deadline := time.NewTimer(time.Second)
				tick := time.NewTicker(time.Millisecond)
				for m.profileWorker.TryLock() {
					m.profileWorker.Unlock()
					select {
					case <-tick.C:
					case <-deadline.C:
						cancel()
						release()
						<-done
						tick.Stop()
						t.Fatal("worker never acquired its first lock")
					}
				}
				tick.Stop()
				deadline.Stop()
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled lock wait lost cancellation", err)
				}
			case <-time.After(time.Second):
				release()
				<-done
				t.Fatal("cancelled worker waited for held lock")
			}
			if starts != 0 || stops != 0 || !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("lock cancellation changed durable intent or performed effects")
			}
			release()
			if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
				t.Fatal("cancelled acquisition leaked a partial lock", err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || starts != 1 || stops != 0 || m.store.Read().RPCState.NetworkPreferenceChange != nil {
				t.Fatal("pending journal did not retry after cancellation", err)
			}
		})
	}
}

func TestProfileWorkerShutdownWhileEffectLockHeld(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	op, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	lock := &sync.Mutex{}
	lock.Lock()
	var unlock sync.Once
	release := func() { unlock.Do(lock.Unlock) }
	defer release()
	stops := 0
	driver := ClientRPCProfileDriver{Lock: lock, Start: func(context.Context, Config) error { t.Error("disconnect started a runtime"); return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	s := NewClientRPCService(m, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done, err := s.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	// Establish that reconciliation owns its worker mutex and is waiting for
	// the held effect lock before requesting host shutdown.
	deadline := time.NewTimer(time.Second)
	tick := time.NewTicker(time.Millisecond)
	for m.disconnectWorker.TryLock() {
		m.disconnectWorker.Unlock()
		select {
		case <-tick.C:
		case <-deadline.C:
			cancel()
			release()
			select {
			case <-done:
			case <-time.After(time.Second):
				tick.Stop()
				t.Fatal("profile worker did not join after failure cleanup")
			}
			tick.Stop()
			t.Fatal("profile worker never entered disconnect reconciliation")
		}
	}
	tick.Stop()
	deadline.Stop()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("host worker shutdown waited for effect lock")
	}
	if stops != 0 || m.store.Read().RPCState.DisconnectOperationID != op.Id || !reflect.DeepEqual(before.ConnectionIntent, m.store.Read().ConnectionIntent) {
		t.Fatal("shutdown consumed pending disconnect")
	}
	release()
	if err := m.ReconcileDisconnect(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || stops != 1 {
		t.Fatal("disconnect did not resume after host shutdown", err)
	}
}
