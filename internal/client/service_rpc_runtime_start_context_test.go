package client

import (
	"reflect"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRuntimeStartMissingMapProjectionAndMutationAreConsistent(t *testing.T) {
	m, owner, ref := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.CachedMap = nil
		profile := cfg.RPCState.Profiles[ref.ProfileId]
		profile.RuntimeStart = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()
		cfg.RPCState.Profiles[ref.ProfileId] = profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: ref})
	if err != nil {
		t.Fatal(err)
	}
	setting := response.Msg.Preferences.Lifecycle.RuntimeStart
	if setting.GetRequested() != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT || setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED || len(setting.AllowedValues) != 0 || setting.Control.Source != ipc.SettingSource_SETTING_SOURCE_UNSPECIFIED || setting.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
		t.Fatal("missing authority was projected as an effective unlocked preference", setting)
	}
	before := m.store.Read()
	_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: ref, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: ref, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("missing policy admission mutated durable settings")
	}
}

func TestRuntimeStartCannotUseMissingActiveProfileOrMap(t *testing.T) {
	for _, scenario := range []string{"missing_profile", "mismatched_profile", "missing_map_keep", "missing_map_connect"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, ref := rpcPreferenceFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
				profile := cfg.RPCState.Profiles[ref.ProfileId]
				switch scenario {
				case "missing_profile":
					delete(cfg.RPCState.Profiles, ref.ProfileId)
				case "mismatched_profile":
					profile.ID = "different"
					cfg.RPCState.Profiles[ref.ProfileId] = profile
				case "missing_map_keep":
					cfg.CachedMap = nil
				case "missing_map_connect":
					cfg.CachedMap = nil
					cfg.ConnectionIntent = nil
					profile.RuntimeStart = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()
					cfg.RPCState.Profiles[ref.ProfileId] = profile
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			after := m.store.Read()
			want := "runtime_start_policy_unavailable"
			if scenario == "missing_profile" || scenario == "mismatched_profile" {
				want = "runtime_start_profile_unavailable"
			}
			if after.ConnectionIntent == nil || after.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || after.ConnectionIntent.Reason != want {
				t.Fatal("missing startup authority allowed automatic connection")
			}
			if !reflect.DeepEqual(before.RPCState, after.RPCState) {
				t.Fatal("startup rewrote recovery state")
			}
			store := reopenRPCStoreFromDisk(t, m.store)
			if err := NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.ConnectionIntent, store.Read().ConnectionIntent) {
				t.Fatal("restart lost blocked intent")
			}
		})
	}
}
