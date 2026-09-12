package client

import (
	"context"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCUIQuitPreferencesAndExecution(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	notify := func() *ipc.Operation {
		t.Helper()
		op, err := m.notifyLifecycleAs(peer, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT})
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	kept := notify()
	if kept.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || kept.GetChange().Changed || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
		t.Fatal("default UI quit disconnected runtime")
	}
	r := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}}
	op, err := m.setPreferencesAs(peer, r)
	if err != nil || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !op.GetChange().Changed {
		t.Fatal("UI quit preference not persisted", err)
	}
	retry, err := m.setPreferencesAs(peer, r)
	if err != nil || !proto.Equal(op, retry) {
		t.Fatal("preference retry changed outcome", err)
	}
	store, err := OpenConfigStore(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if rpcUIQuit(store.Read().RPCState.Profiles[profile.ProfileId]) != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		t.Fatal("UI quit override not durable")
	}
	quit := notify()
	if quit.State != ipc.OperationState_OPERATION_STATE_PENDING || quit.Kind != ipc.OperationKind_OPERATION_KIND_NOTIFY_LIFECYCLE || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("UI quit did not schedule disconnect")
	}
	stops := 0
	if err := m.ReconcileDisconnect(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}); err != nil {
		t.Fatal(err)
	}
	final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: quit.Id}})
	if err != nil || stops != 1 || final.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || final.Kind != ipc.OperationKind_OPERATION_KIND_NOTIFY_LIFECYCLE {
		t.Fatal("UI quit bypassed durable Down", err)
	}
	reset, err := m.resetPreferencesAs(peer, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}})
	if err != nil || !reset.GetChange().Changed || m.store.Read().RPCState.Profiles[profile.ProfileId].UIQuit != nil {
		t.Fatal("reset did not remove override", err)
	}
}

func TestRPCUIQuitRejectsUnsupportedPatchAtomically(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	before := m.Metadata().Revision
	_, err := m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), AcceptDns: proto.Bool(false)}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	if m.Metadata().Revision != before || m.store.Read().RPCState.Profiles[profile.ProfileId].UIQuit != nil {
		t.Fatal("unsupported mixed patch partially applied")
	}
	_, err = m.notifyLifecycleAs(peer, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent(99)})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}

func TestRPCPreferenceChangeInvalidatesEffectiveSettings(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	if _, err := sub.next(ctx); err != nil {
		t.Fatal(err)
	}
	op, err := m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[ipc.Domain]bool{}
	for i := 0; i < 4; i++ {
		event, err := sub.next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Metadata.Revision != op.Metadata.Revision {
			t.Fatal("preference events mixed revisions")
		}
		if invalidation := event.GetInvalidated(); invalidation != nil {
			if invalidation.ProfileId != profile.ProfileId {
				t.Fatal("invalidation lost profile scope")
			}
			seen[invalidation.Domain] = true
		}
	}
	if !seen[ipc.Domain_DOMAIN_PREFERENCES] || !seen[ipc.Domain_DOMAIN_MANAGED_SETTINGS] {
		t.Fatal("effective setting consumers left stale")
	}
}
