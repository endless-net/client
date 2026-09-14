package client

import (
	"reflect"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

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
