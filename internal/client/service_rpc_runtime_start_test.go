package client

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeStartManagedPolicyOverrideResetAndExecution(t *testing.T) {
	for _, source := range []api.ClientPolicySource{api.ClientPolicyAccount, api.ClientPolicyDevice} {
		for _, locked := range []bool{false, true} {
			t.Run(string(source)+strconv.FormatBool(locked), func(t *testing.T) {
				m, owner, profile := rpcPreferenceFixture(t)
				opts, key := signedServiceDNSFixture(t)
				behavior := api.ClientLifecycleDisconnect
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingRuntimeStart, Source: source, PolicyID: "startup-policy", Locked: locked, LifecycleValue: &behavior}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
				if err := m.store.Update(func(cfg *Config) error {
					cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				read := func() *ipc.LifecycleSetting {
					t.Helper()
					response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
					if err != nil {
						t.Fatal(err)
					}
					return response.Msg.Preferences.Lifecycle.RuntimeStart
				}
				setting := read()
				wantSource := ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
				if source == api.ClientPolicyDevice {
					wantSource = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY
				}
				if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || setting.Requested != nil || setting.Control.Source != wantSource || setting.Control.PolicyId != "startup-policy" || setting.Control.Locked != locked {
					t.Fatal("policy projection lost", setting)
				}
				before := m.store.Read()
				request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum(), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}}
				op, err := m.setPreferencesAs(owner, request)
				if locked {
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
					if !reflect.DeepEqual(before, m.store.Read()) {
						t.Fatal("rejected lock partially committed patch")
					}
				} else {
					if err != nil || op.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
						t.Fatal(err, op)
					}
					if !reflect.DeepEqual(before.ConnectionIntent, m.store.Read().ConnectionIntent) {
						t.Fatal("preference mutation executed a future startup event")
					}
					if got := read(); got.GetRequested() != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT || got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT || got.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER {
						t.Fatal(got)
					}
					m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
					if err != nil {
						t.Fatal(err)
					}
					replay, err := m.setPreferencesAs(owner, request)
					if err != nil || !proto.Equal(op, replay) {
						t.Fatal("startup preference replay changed", err)
					}
					if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
						t.Fatal(err)
					}
					if m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
						t.Fatal("startup did not execute local CONNECT")
					}
				}
				_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START}})
				if err != nil {
					t.Fatal(err)
				}
				if got := read(); got.Requested != nil || got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || got.Control.Source != wantSource {
					t.Fatal("reset did not restore policy", got)
				}
				if !locked && m.store.Read().RPCState.Profiles[profile.ProfileId].UIQuit == nil {
					t.Fatal("selective startup reset removed another lifecycle override")
				}
				if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
				if m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
					t.Fatal("startup did not execute managed DISCONNECT")
				}
			})
		}
	}
}

func TestRuntimeStartRejectsSourceFailuresWithoutDisablingLocalRecovery(t *testing.T) {
	for _, scenario := range []string{"tampered", "expired", "recipient", "revision", "trust", "locked_connect"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			opts, key := signedServiceDNSFixture(t)
			behavior := api.ClientLifecycleConnect
			opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingRuntimeStart, Source: api.ClientPolicyDevice, PolicyID: "startup", Locked: true, LifecycleValue: &behavior}}}
			resignApplicationMap(t, &opts.NetworkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				p := cfg.RPCState.Profiles[profile.ProfileId]
				p.RuntimeStart = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
				cfg.RPCState.Profiles[p.ID] = p
				switch scenario {
				case "tampered":
					cfg.CachedMap.Network.Name = "tampered"
				case "recipient":
					cfg.NodeID = "other"
				case "revision":
					cfg.MapRevision++
				case "trust":
					cfg.MapSigningTrust = nil
				case "locked_connect":
					cfg.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			store := NewConnectionIntentStore(m.store)
			if scenario == "expired" {
				store.now = func() time.Time { return opts.NetworkMap.MapSignature.ExpiresAt }
				m.now = store.now
			}
			if err := store.InitializeRuntimeIntent(); err != nil {
				t.Fatal("source failure prevented local recovery startup", err)
			}
			cfg := m.store.Read()
			response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
			if scenario == "locked_connect" {
				if err != nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected || response.Msg.Preferences.Lifecycle.RuntimeStart.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT {
					t.Fatal("managed CONNECT did not override local value", err)
				}
			} else {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
				if cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.ConnectionIntent.Reason != "runtime_start_policy_unavailable" {
					t.Fatal("untrusted policy permitted startup networking")
				}
				_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()}})
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
		})
	}
}

func TestRuntimeStartDoesNotBypassPendingDisconnectOrOwner(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()}}
	_, err := m.setPreferencesAs(local.Peer{Identity: "other"}, request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if _, err := m.setPreferencesAs(owner, request); err != nil {
		t.Fatal(err)
	}
	if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	before := m.store.Read().ConnectionIntent
	if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.store.Read().ConnectionIntent) || m.store.Read().ConnectionIntent.Reason != "user_disconnect" {
		t.Fatal("startup CONNECT superseded pending disconnect")
	}
}
