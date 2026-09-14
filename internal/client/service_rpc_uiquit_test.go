package client

import (
	"context"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCPreferencesRequireActiveProfileForMutation(t *testing.T) {
	m, peer, active := rpcPreferenceFixture(t)
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://other.test"
	created, err := m.createProfileAs(peer, create)
	if err != nil {
		t.Fatal(err)
	}
	inactive := &ipc.ProfileRef{ProfileId: created.ProfileId}
	before := m.Metadata().Revision
	s := NewClientRPCService(m, nil)
	for _, ref := range []*ipc.ProfileRef{active, inactive} {
		response, err := s.preferencesAs(peer, &ipc.GetPreferencesRequest{Profile: ref})
		if err != nil {
			t.Fatal(err)
		}
		setting := response.Msg.Preferences.Lifecycle.UiQuit
		restriction := setting.Control.Mutation
		if ref == active {
			if restriction.Availability != ipc.Availability_AVAILABILITY_AVAILABLE || restriction.ReasonKey != "" {
				t.Fatal("active profile preference is not available")
			}
		} else if restriction.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE || restriction.ReasonKey != "preference_requires_active_profile" || restriction.ActionOwner != ipc.ActionOwner_ACTION_OWNER_USER {
			t.Fatal("inactive profile preference advertises a forbidden mutation")
		}
		if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || setting.Requested != nil || setting.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT || response.Msg.Preferences.Metadata.Revision != before {
			t.Fatal("mutation restriction changed preference value or snapshot")
		}
	}
	_, err = m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: inactive, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.resetPreferencesAs(peer, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: inactive, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	cfg := m.store.Read()
	if m.Metadata().Revision != before || cfg.RPCState.Profiles[inactive.ProfileId].UIQuit != nil || cfg.RPCState.ActiveProfileID != active.ProfileId {
		t.Fatal("rejected inactive preference mutation changed state")
	}
	_, err = m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: active, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	if err != nil {
		t.Fatal(err)
	}
	if m.store.Read().RPCState.Profiles[inactive.ProfileId].UIQuit != nil {
		t.Fatal("active preference leaked into another profile")
	}
}

func TestRPCUIQuitPreferencesAndExecution(t *testing.T) {
	m, peer, profile := rpcPreferenceFixture(t)
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
	store := reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if rpcUIQuit(store.Read().RPCState.Profiles[profile.ProfileId]) != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		t.Fatal("UI quit override not durable")
	}
	retry, err = m.setPreferencesAs(peer, r)
	if err != nil || !proto.Equal(op, retry) {
		t.Fatal("disk-backed preference replay changed outcome")
	}
	quit := notify()
	if quit.State != ipc.OperationState_OPERATION_STATE_PENDING || quit.Kind != ipc.OperationKind_OPERATION_KIND_NOTIFY_LIFECYCLE || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("UI quit did not schedule disconnect")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
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
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	kept = notify()
	if kept.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED || kept.GetChange().GetChanged() ||
		rpcUIQuit(m.store.Read().RPCState.Profiles[profile.ProfileId]) != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT ||
		m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || stops != 1 {
		t.Fatal("reset UI quit preference restored an override or reconnected the runtime")
	}
}

func TestRPCUIQuitRejectsUnsupportedPatchAtomically(t *testing.T) {
	m, peer, profile := rpcPreferenceFixture(t)
	before := m.Metadata().Revision
	_, err := m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), AcceptDns: proto.Bool(false)}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	if m.Metadata().Revision != before || m.store.Read().RPCState.Profiles[profile.ProfileId].UIQuit != nil {
		t.Fatal("unsupported mixed patch partially applied")
	}
	_, err = m.notifyLifecycleAs(peer, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent(99)})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}

func TestRPCUIQuitReplayDoesNotApplyChangedPreference(t *testing.T) {
	m, peer, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT}
	original, err := m.notifyLifecycleAs(peer, request)
	if err != nil || original.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED || original.GetChange().GetChanged() {
		t.Fatal("initial keep-intent notification failed", err)
	}
	_, err = m.setPreferencesAs(peer, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	if err != nil {
		t.Fatal(err)
	}
	for _, restart := range []bool{false, true} {
		if restart {
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
		}
		revision := m.Metadata().Revision
		replayed, err := m.notifyLifecycleAs(peer, request)
		if err != nil || !proto.Equal(original, replayed) {
			t.Fatalf("restart=%t: original lifecycle outcome was not replayed: %v", restart, err)
		}
		if m.Metadata().Revision != revision || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
			t.Fatalf("restart=%t: replay applied the newer disconnect preference", restart)
		}
	}
	// A genuinely new notification must still execute the current preference.
	fresh, err := m.notifyLifecycleAs(peer, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT})
	if err != nil || fresh.GetState() != ipc.OperationState_OPERATION_STATE_PENDING || fresh.GetId() == original.GetId() || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("new lifecycle notification did not apply current preference", err)
	}
}

func TestRPCPreferenceChangeInvalidatesEffectiveSettings(t *testing.T) {
	m, peer, profile := rpcPreferenceFixture(t)
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
	for i := 0; i < 5; i++ {
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
	if !seen[ipc.Domain_DOMAIN_PREFERENCES] || !seen[ipc.Domain_DOMAIN_MANAGED_SETTINGS] || !seen[ipc.Domain_DOMAIN_RESOURCES] {
		t.Fatal("effective setting consumers left stale")
	}
}
