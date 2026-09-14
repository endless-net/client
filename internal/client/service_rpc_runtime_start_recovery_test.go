package client

import (
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestRuntimeStartupRecoveryKeepsOriginalIntentWithoutRevivingNewDisconnect(t *testing.T) {
	for _, scenario := range []string{"restore", "user_disconnect", "owner_changed", "credential_changed", "managed_disconnect", "enrollment_recovery"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			valid := m.store.Read().CachedMap
			original := &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect", UpdatedAt: "2026-09-14T00:00:00Z"}
			if err := m.store.Update(func(cfg *Config) error { cfg.ConnectionIntent = original; cfg.CachedMap = nil; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			blocked := m.store.Read().ConnectionIntent
			if blocked.DesiredState != ConnectionIntentDesiredDisconnected || blocked.StartupRecovery == nil {
				t.Fatal("startup lost original intent while blocking")
			}
			store := reopenRPCStoreFromDisk(t, m.store)
			if err := NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(blocked, store.Read().ConnectionIntent) {
				t.Fatal("offline restart replaced the original checkpoint")
			}
			if scenario == "user_disconnect" {
				if err := NewConnectionIntentStore(store).SetDisconnected("user_disconnect"); err != nil {
					t.Fatal(err)
				}
			}
			trust := store.Read().MapSigningTrust
			if scenario == "managed_disconnect" {
				opts, key := signedServiceDNSFixture(t)
				behavior := api.ClientLifecycleDisconnect
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingRuntimeStart, Source: api.ClientPolicyDevice, PolicyID: "startup", Locked: true, LifecycleValue: &behavior}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
				valid, trust = &opts.NetworkMap, opts.SigningTrust
			}
			if err := store.Update(func(cfg *Config) error {
				cfg.CachedMap = valid
				cfg.MapSigningTrust = trust
				if scenario == "enrollment_recovery" {
					cfg.EnrollmentRecovery = &EnrollmentRecovery{Phase: RecoveryPhaseNeedsLogin}
				}
				if scenario == "owner_changed" {
					cfg.LocalOwnerID = "other"
				}
				if scenario == "credential_changed" {
					cfg.NodeCredential = "replacement"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			final := store.Read().ConnectionIntent
			if scenario == "enrollment_recovery" {
				if !reflect.DeepEqual(final, blocked) {
					t.Fatal("restart bypassed retained enrollment recovery")
				}
				return
			}
			if final.StartupRecovery != nil {
				t.Fatal("resolved or invalidated startup retained checkpoint")
			}
			if scenario == "restore" {
				if !reflect.DeepEqual(final, original) {
					t.Fatal("fresh policy did not restore original KEEP_INTENT", final)
				}
			} else if final.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("startup revived superseded intent")
			}
		})
	}
}
