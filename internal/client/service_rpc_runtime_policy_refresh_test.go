package client

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRuntimePolicyRefreshCASAndAuthority(t *testing.T) {
	for _, scenario := range []string{"success", "tampered", "expired", "hash", "stale", "cancelled", "disconnect", "owner_changed"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			valid := m.store.Read().CachedMap
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap = nil
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			candidate := m.store.Read()
			candidate.CachedMap = valid
			candidate.MapHash = valid.MapSignature.PayloadHash
			// Fetch results cannot replace local intent or identity.
			candidate.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
			candidate.LocalOwnerID = "untrusted-candidate-owner"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "tampered":
				candidate.CachedMap.Network.Name = "tampered"
			case "expired":
				m.now = func() time.Time { return valid.MapSignature.ExpiresAt }
			case "hash":
				candidate.MapHash = "wrong"
			case "stale":
				candidate.MapRevision = 0
			case "cancelled":
				cancel()
			case "disconnect", "owner_changed":
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "disconnect" {
						cfg.ConnectionIntent.Reason = "newer_disconnect"
					} else {
						cfg.LocalOwnerID = "new-owner"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			unchanged := m.store.Read()
			err := m.RefreshRuntimeLifecyclePolicy(ctx, before, candidate)
			if scenario != "success" {
				if err == nil || !reflect.DeepEqual(unchanged, m.store.Read()) {
					t.Fatal("rejected refresh mutated state", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			after := m.store.Read()
			if after.CachedMap == nil || after.LocalOwnerID != before.LocalOwnerID || !reflect.DeepEqual(after.ConnectionIntent, before.ConnectionIntent) || after.RPCState.Revision != before.RPCState.Revision+1 {
				t.Fatal("authority refresh overwrote local state or missed revision")
			}
			if err := m.RefreshRuntimeLifecyclePolicy(ctx, after, after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(clonePersistentConfig(after), clonePersistentConfig(m.store.Read())) {
				t.Fatal("identical refresh changed revision")
			}
		})
	}
}
