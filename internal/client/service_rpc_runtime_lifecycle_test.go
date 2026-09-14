package client

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRuntimeLifecycleDisconnectCannotBeUndoneByProfileSwitch(t *testing.T) {
	m, _, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		active := cfg.RPCState.Profiles[profile.ProfileId]
		active.Suspend = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
		cfg.RPCState.Profiles[active.ID] = active
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.RPCState.Profiles["target"] = clientRPCProfile{ID: "target", Configuration: Config{ConnectionIntent: &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}}}
		cfg.RPCState.ProfileSwitch = &clientRPCProfileSwitch{From: active.ID, To: "target"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyRuntimeLifecycleIntent(t.Context(), RuntimeSuspend, ""); err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	if cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || !reflect.DeepEqual(cfg.ConnectionIntent, cfg.RPCState.Profiles["target"].Configuration.ConnectionIntent) {
		t.Fatal("pending switch retained reconnect intent after lifecycle disconnect")
	}
	before := cfg.RPCState.Revision
	if err := m.ApplyRuntimeLifecycleIntent(t.Context(), RuntimeSuspend, ""); err != nil || m.store.Read().RPCState.Revision != before {
		t.Fatal("duplicate disconnect rewrote durable intent", err)
	}
	if err := m.ApplyRuntimeLifecycleIntent(t.Context(), RuntimeResume, ""); err != nil || !reflect.DeepEqual(cfg.ConnectionIntent, m.store.Read().ConnectionIntent) {
		t.Fatal("resume KEEP_INTENT restored an older connection", err)
	}
}

func TestRuntimeLifecyclePreservesCurrentIntentAcrossRestart(t *testing.T) {
	for _, event := range []RuntimeLifecycleEvent{RuntimeUserLogoff, RuntimeSuspend, RuntimeResume} {
		for _, desired := range []string{"", ConnectionIntentDesiredConnected, ConnectionIntentDesiredDisconnected} {
			t.Run(fmt.Sprint(event)+"/"+desired, func(t *testing.T) {
				m, owner, _ := rpcPreferenceFixture(t)
				if err := m.store.Update(func(cfg *Config) error {
					cfg.ConnectionIntent = nil
					if desired != "" {
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: desired, Reason: "user_choice", UpdatedAt: "saved-time"}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				before := m.store.Read()
				if err := m.ApplyRuntimeLifecycleIntent(t.Context(), event, owner.Identity); err != nil {
					t.Fatal(err)
				}
				got := m.store.Read()
				if desired == "" {
					if got.ConnectionIntent == nil || got.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
						t.Fatal("no saved intent implied connect")
					}
				} else if !reflect.DeepEqual(before.ConnectionIntent, got.ConnectionIntent) || before.RPCState.Revision != got.RPCState.Revision {
					t.Fatal("KEEP_INTENT rewrote current user choice")
				}
				m, err := NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				if err := m.ApplyRuntimeLifecycleIntent(t.Context(), event, owner.Identity); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.ConnectionIntent, m.store.Read().ConnectionIntent) {
					t.Fatal("restart or duplicate event changed intent")
				}
			})
		}
	}
}

func TestRuntimeLifecycleUsesSignedPolicyBeforeCancellingApply(t *testing.T) {
	for _, entry := range []struct {
		event RuntimeLifecycleEvent
		key   api.ClientSettingKey
	}{
		{RuntimeUserLogoff, api.ClientSettingUserLogoff}, {RuntimeSuspend, api.ClientSettingSuspend}, {RuntimeResume, api.ClientSettingResume},
	} {
		for _, scenario := range []string{"disconnect", "tampered", "missing", "cancelled", "owner", "invalid_event", "saved_recovery"} {
			t.Run(string(entry.key)+"/"+scenario, func(t *testing.T) {
				m, owner, _ := rpcPreferenceFixture(t)
				opts, key := signedServiceDNSFixture(t)
				behavior := api.ClientLifecycleDisconnect
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: entry.key, Source: api.ClientPolicyDevice, PolicyID: "lifecycle-policy", Locked: true, LifecycleValue: &behavior}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
				if err := m.store.Update(func(cfg *Config) error {
					cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
					if scenario == "tampered" {
						cfg.CachedMap.Network.Name = "tampered"
					}
					if scenario == "missing" {
						cfg.NodeCredential = "synthetic-credential"
						cfg.CachedMap = nil
					}
					if scenario == "saved_recovery" {
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "runtime_start_policy_unavailable", StartupRecovery: &clientRuntimeStartRecovery{PreviousState: ConnectionIntentDesiredConnected}}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				before := m.store.Read()
				cancelled := false
				m.cancelApply = func() {
					cancelled = true
					cfg := m.store.Read()
					if cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.ConnectionIntent.StartupRecovery != nil {
						t.Fatal("cancel preceded durable intent")
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if scenario == "cancelled" {
					cancel()
				}
				actor, event := owner.Identity, entry.event
				if scenario == "owner" {
					actor = "other"
					event = RuntimeUserLogoff
				}
				if scenario == "invalid_event" {
					event = 0
				}
				err := m.ApplyRuntimeLifecycleIntent(ctx, event, actor)
				if scenario == "disconnect" || scenario == "saved_recovery" {
					if err != nil || !cancelled || m.store.Read().RPCState.Revision != before.RPCState.Revision+1 {
						t.Fatal("policy effect was not committed atomically", err)
					}
					if m.store.Read().RPCState.DisconnectOperationID != "" {
						t.Fatal("OS event fabricated a public command")
					}
				} else if err == nil || cancelled || !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("rejected event changed runtime", err)
				}
			})
		}
	}
}
