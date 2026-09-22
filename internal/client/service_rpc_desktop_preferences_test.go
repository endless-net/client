package client

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestDesktopLifecyclePreferencesPolicyPersistenceAndReset(t *testing.T) {
	for _, entry := range []struct {
		key    ipc.PreferenceKey
		policy api.ClientSettingKey
		patch  func(*ipc.PreferencesPatch, ipc.LifecycleBehavior)
		read   func(*ipc.RuntimeLifecycle) *ipc.LifecycleSetting
	}{
		{ipc.PreferenceKey_PREFERENCE_KEY_USER_LOGOFF, api.ClientSettingUserLogoff, func(p *ipc.PreferencesPatch, v ipc.LifecycleBehavior) { p.UserLogoff = v.Enum() }, func(v *ipc.RuntimeLifecycle) *ipc.LifecycleSetting { return v.UserLogoff }},
		{ipc.PreferenceKey_PREFERENCE_KEY_SUSPEND, api.ClientSettingSuspend, func(p *ipc.PreferencesPatch, v ipc.LifecycleBehavior) { p.Suspend = v.Enum() }, func(v *ipc.RuntimeLifecycle) *ipc.LifecycleSetting { return v.Suspend }},
		{ipc.PreferenceKey_PREFERENCE_KEY_RESUME, api.ClientSettingResume, func(p *ipc.PreferencesPatch, v ipc.LifecycleBehavior) { p.Resume = v.Enum() }, func(v *ipc.RuntimeLifecycle) *ipc.LifecycleSetting { return v.Resume }},
	} {
		for _, source := range []api.ClientPolicySource{api.ClientPolicyAccount, api.ClientPolicyDevice} {
			for _, locked := range []bool{false, true} {
				t.Run(string(entry.policy)+"/"+string(source)+"/"+strconv.FormatBool(locked), func(t *testing.T) {
					m, owner, profile := rpcPreferenceFixture(t)
					read := func() *ipc.LifecycleSetting {
						t.Helper()
						response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
						if err != nil {
							t.Fatal(err)
						}
						return entry.read(response.Msg.Preferences.Lifecycle)
					}
					if got := read(); got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || got.Requested != nil || got.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT {
						t.Fatal("desktop default must preserve intent", got)
					}
					localPatch := &ipc.PreferencesPatch{}
					entry.patch(localPatch, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT)
					if _, err := m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: localPatch}); err != nil {
						t.Fatal(err)
					}
					if _, err := m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{entry.key}}); err != nil {
						t.Fatal(err)
					}
					if got := read(); got.Requested != nil || got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || got.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT {
						t.Fatal("reset did not restore accepted default", got)
					}
					opts, key := signedServiceDNSFixture(t)
					behavior := api.ClientLifecycleDisconnect
					opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: entry.policy, Source: source, PolicyID: "desktop-policy", Locked: locked, LifecycleValue: &behavior}}}
					resignApplicationMap(t, &opts.NetworkMap, key)
					if err := m.store.Update(func(cfg *Config) error {
						cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					wantSource := ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
					if source == api.ClientPolicyDevice {
						wantSource = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY
					}
					if got := read(); got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || got.Control.Source != wantSource || got.Control.Locked != locked || got.Control.PolicyId != "desktop-policy" {
						t.Fatal("lost authenticated policy", got)
					}
					before := m.store.Read()
					patch := &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}
					entry.patch(patch, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT)
					request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: patch}
					op, err := m.setPreferencesAs(owner, request)
					if locked {
						assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
						if !reflect.DeepEqual(before, m.store.Read()) {
							t.Fatal("policy rejection partially committed patch")
						}
						patch.AcceptDns = proto.Bool(false)
						_, err = m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: patch})
						assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
						if !reflect.DeepEqual(before, m.store.Read()) {
							t.Fatal("locked desktop policy admitted mixed network patch")
						}
					} else {
						if err != nil || op.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !op.GetChange().GetChanged() {
							t.Fatal(err, op)
						}
						m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
						if err != nil {
							t.Fatal(err)
						}
						replay, err := m.setPreferencesAs(owner, request)
						if err != nil || !proto.Equal(op, replay) {
							t.Fatal("restart replay changed", err)
						}
						if got := read(); got.GetRequested() != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || got.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER {
							t.Fatal(got)
						}
					}
					if !reflect.DeepEqual(before.ConnectionIntent, m.store.Read().ConnectionIntent) {
						t.Fatal("preference mutation executed an OS event")
					}
					_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{entry.key}})
					if err != nil {
						t.Fatal(err)
					}
					if got := read(); got.Requested != nil || got.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || got.Control.Source != wantSource {
						t.Fatal("reset lost policy", got)
					}
					if !locked && m.store.Read().RPCState.Profiles[profile.ProfileId].UIQuit == nil {
						t.Fatal("selective reset erased another preference")
					}
				})
			}
		}
	}
}

func TestUnavailableLifecycleSourcesDoNotAdvertiseOrAcceptEventPreferences(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	m.SetRuntimeLifecycleSources(false, false)
	response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []*ipc.LifecycleSetting{response.Msg.Preferences.Lifecycle.UserLogoff, response.Msg.Preferences.Lifecycle.Suspend, response.Msg.Preferences.Lifecycle.Resume} {
		if setting.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_UNSUPPORTED || setting.Control.Mutation.ReasonKey != "lifecycle_source_unsupported" || len(setting.AllowedValues) != 0 {
			t.Fatal("preference advertised an absent OS event source", setting)
		}
	}
	for _, setting := range []*ipc.LifecycleSetting{response.Msg.Preferences.Lifecycle.UiQuit, response.Msg.Preferences.Lifecycle.RuntimeStart} {
		if setting.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
			t.Fatal("source gating disabled an independent lifecycle preference", setting)
		}
	}
	before := m.store.Read()
	_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Suspend: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_USER_LOGOFF}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("unsupported lifecycle source partially changed profile")
	}
}

func TestDesktopLifecyclePreferencesRejectUnknownPolicyAndUnauthorizedMutation(t *testing.T) {
	for _, scenario := range []string{"tampered", "expired", "missing", "owner", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			patch := &ipc.PreferencesPatch{UserLogoff: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Suspend: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Resume: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}
			if err := m.store.Update(func(cfg *Config) error {
				switch scenario {
				case "tampered":
					cfg.CachedMap.Network.Name = "tampered"
				case "missing":
					cfg.NodeCredential = "synthetic-test-credential"
					cfg.CachedMap = nil
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				expiry := m.store.Read().CachedMap.MapSignature.ExpiresAt
				m.now = func() time.Time { return expiry }
			}
			want := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			if scenario == "owner" {
				owner.Identity = "other"
				want = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			}
			if scenario == "unsupported" {
				patch.Resume = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()
				want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			}
			before := m.store.Read()
			_, err := m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: patch})
			assertRPCFailure(t, err, want)
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("rejected desktop patch changed durable state")
			}
			if scenario == "missing" {
				response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []*ipc.LifecycleSetting{response.Msg.Preferences.Lifecycle.UserLogoff, response.Msg.Preferences.Lifecycle.Suspend, response.Msg.Preferences.Lifecycle.Resume} {
					if value.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED || len(value.AllowedValues) != 0 || value.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
						t.Fatal("missing policy fabricated a desktop default", value)
					}
				}
			}
		})
	}
}
