package client

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func rpcPreferenceFixture(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.ProfileRef) {
	t.Helper()
	m, owner, profile := rpcConnectFixture(t)
	opts, _ := signedServiceDNSFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeID, cfg.NetworkID = opts.NetworkMap.Node.ID, opts.NetworkMap.Network.ID
		cfg.MapRevision, cfg.MapGlobalRevision = opts.NetworkMap.Network.Revision, opts.NetworkMap.Revision.Global
		cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, owner, profile
}

func TestRPCUIQuitManagedBaselineLockAndReset(t *testing.T) {
	for _, source := range []api.ClientPolicySource{api.ClientPolicyAccount, api.ClientPolicyDevice} {
		for _, locked := range []bool{false, true} {
			t.Run(string(source)+"/locked="+strconv.FormatBool(locked), func(t *testing.T) {
				m, owner, profile := rpcConnectFixture(t)
				opts, key := signedServiceDNSFixture(t)
				behavior := api.ClientLifecycleDisconnect
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingUIQuit, Source: source, PolicyID: "quit-policy", Locked: locked, LifecycleValue: &behavior}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
				if err := m.store.Update(func(cfg *Config) error {
					cfg.NodeID, cfg.NetworkID = opts.NetworkMap.Node.ID, opts.NetworkMap.Network.ID
					cfg.MapRevision, cfg.MapGlobalRevision = opts.NetworkMap.Network.Revision, opts.NetworkMap.Revision.Global
					cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
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
					return response.Msg.Preferences.Lifecycle.UiQuit
				}
				setting := read()
				wantSource := ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
				actor := ipc.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
				if source == api.ClientPolicyDevice {
					wantSource, actor = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY, ipc.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
				}
				if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || setting.Requested != nil || setting.Control.Source != wantSource || setting.Control.PolicyId != "quit-policy" || setting.Control.Locked != locked {
					t.Fatal("managed baseline/provenance lost", setting)
				}
				if locked && (setting.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_POLICY_BLOCKED || setting.Control.Mutation.ActionOwner != actor || len(setting.AllowedValues) != 1) {
					t.Fatal("managed lock projection lost", setting)
				}
				before := m.store.Read()
				_, err := m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()}})
				if locked {
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
					if !reflect.DeepEqual(before, m.store.Read()) {
						t.Fatal("lock rejection changed durable state")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					setting = read()
					if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || setting.Requested == nil || setting.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER || setting.Control.PolicyId != "quit-policy" {
						t.Fatal("unlocked override lost baseline provenance", setting)
					}
				}
				// An equal value is permitted by the producer lock; reset removes
				// only the override and must restore policy, not the local default.
				_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}})
				if err != nil {
					t.Fatal(err)
				}
				_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}})
				if err != nil {
					t.Fatal(err)
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				setting = read()
				if setting.Requested != nil || setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || setting.Control.Source != wantSource {
					t.Fatal("restart/reset lost managed baseline", setting)
				}
				op, err := m.notifyLifecycleAs(owner, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT})
				if err != nil || op.GetState() != ipc.OperationState_OPERATION_STATE_PENDING || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
					t.Fatal("managed quit did not durably schedule Down", err)
				}
				stops := 0
				if err := m.ReconcileDisconnect(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					stops++
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
				}}); err != nil {
					t.Fatal(err)
				}
				final, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
				if err != nil || stops != 1 || final.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
					t.Fatal("managed quit bypassed executor result", err)
				}
			})
		}
	}
}

func TestRPCUIQuitRejectsInvalidPolicyAndReplaysAcceptedRequest(t *testing.T) {
	for _, scenario := range []string{"tampered", "expired", "recipient", "revision", "trust", "unsupported", "locked_override"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			opts, key := signedServiceDNSFixture(t)
			if scenario == "expired" {
				// Expire the policy while the accepted operation is still within
				// its replay retention period, independent of signing latency.
				m.now = func() time.Time { return opts.NetworkMap.MapSignature.ExpiresAt.Add(-time.Minute) }
			}
			request := &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT}
			original, err := m.notifyLifecycleAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			behavior := api.ClientLifecycleDisconnect
			if scenario == "unsupported" {
				behavior = api.ClientLifecycleConnect
			}
			opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingUIQuit, Source: api.ClientPolicyAccount, PolicyID: "new-policy", Locked: true, LifecycleValue: &behavior}}}
			resignApplicationMap(t, &opts.NetworkMap, key)
			if scenario == "tampered" {
				opts.NetworkMap.Network.ClientPolicy.Settings[0].Locked = false
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = opts.NetworkMap.Node.ID, opts.NetworkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = opts.NetworkMap.Network.Revision, opts.NetworkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
				switch scenario {
				case "recipient":
					cfg.NodeID = "other"
				case "revision":
					cfg.MapRevision++
				case "trust":
					cfg.MapSigningTrust = nil
				case "locked_override":
					p := cfg.RPCState.Profiles[profile.ProfileId]
					p.UIQuit = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()
					cfg.RPCState.Profiles[p.ID] = p
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, restart := range []bool{false, true} {
				if restart {
					m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
					if err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "expired" {
					m.now = func() time.Time { return opts.NetworkMap.MapSignature.ExpiresAt }
				}
				before := m.store.Read()
				replayed, err := m.notifyLifecycleAs(owner, request)
				if err != nil || !proto.Equal(original, replayed) || !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("policy change or expiry broke durable replay", err)
				}
				response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
				if scenario == "locked_override" {
					if err != nil || response.Msg.Preferences.Lifecycle.UiQuit.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT || *response.Msg.Preferences.Lifecycle.UiQuit.Requested != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT {
						t.Fatal("new lock did not constrain existing override", err)
					}
					continue
				}
				want := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
				if scenario == "unsupported" {
					want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				}
				assertRPCFailure(t, err, want)
				if response != nil {
					t.Fatal("invalid policy read returned settings")
				}
				_, err = m.notifyLifecycleAs(owner, &ipc.NotifyLifecycleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Event: ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT})
				assertRPCFailure(t, err, want)
				_, err = m.setPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()}})
				assertRPCFailure(t, err, want)
				_, err = m.resetPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}})
				assertRPCFailure(t, err, want)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("invalid policy changed durable state")
				}
			}
		})
	}
}
