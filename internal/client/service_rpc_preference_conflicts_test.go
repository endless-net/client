package client

import (
	"context"
	"reflect"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestUIQuitMutationCannotBypassPendingNetworkPatch(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	oldRequest := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()}}
	old, err := m.setPreferencesAs(owner, oldRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	if err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	replay, err := m.setPreferencesAs(owner, oldRequest)
	if err != nil || !proto.Equal(old, replay) {
		t.Fatal("pending patch blocked accepted replay", err)
	}
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("conflict or replay changed pending patch")
	}
	response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	quit := response.Msg.Preferences.Lifecycle.UiQuit
	if quit.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || quit.GetRequested() != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || quit.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
		t.Fatal("pending UI-quit projection permits conflicting mutation", quit)
	}
}

func TestNetworkPreferenceMutationProjectionTracksWorkerReadiness(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	read := func(want ipc.Availability) {
		t.Helper()
		response, err := s.preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []*ipc.BooleanSetting{response.Msg.Preferences.AcceptDns, response.Msg.Preferences.AcceptRoutes} {
			if value.Control.Mutation.Availability != want || !value.Effective || value.Requested != nil {
				t.Fatal("worker readiness changed value or advertised wrong mutation availability", value)
			}
		}
	}
	read(ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.profileWorker = &clientRPCProfileWorker{ctx: ctx}
	read(ipc.Availability_AVAILABILITY_AVAILABLE)
	cancel()
	read(ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE)
}
