package client

import (
	"context"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestNativeExitLANCleanupRetiresOnlyConfirmedMatchingJournal(t *testing.T) {
	for _, scenario := range []string{"success", "native_error", "cancel", "replacement", "changed_plan"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, scope := rpcExitLANOwnershipFixture(t)
			manifest := exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
			if err := m.checkpointExitLANOwnership(t.Context(), id, scope, manifest); err != nil {
				t.Fatal(err)
			}
			scope = cloneExitProtection(m.store.Read().RPCState.ExitProtection)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			guard := &linuxExitGuard{}
			n := &nativeExitExecutor{store: m.store, cleanupLANObjects: func(_ context.Context, actual *linuxExitGuard, owned *exitLANOwnership) error {
				calls++
				if actual != guard || !reflect.DeepEqual(owned, manifest) {
					t.Fatal("cleanup scope changed")
				}
				if m.store.Read().RPCState.ExitProtection.LAN == nil {
					t.Fatal("journal removed before native absence")
				}
				if scenario == "native_error" {
					return errExitLANBPF
				}
				if scenario == "cancel" {
					cancel()
				}
				if scenario == "replacement" || scenario == "changed_plan" {
					if err := m.store.Update(func(cfg *Config) error {
						if scenario == "replacement" {
							cfg.RPCState.ExitProtection.OperationID = "replacement"
						} else {
							cfg.RPCState.ExitChange.Protection.OperationID = "replacement"
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}}
			err := n.cleanupLAN(ctx, scope, guard)
			if (err == nil) != (scenario == "success") || calls != 1 {
				t.Fatal("unexpected cleanup outcome", err)
			}
			reopened := reopenRPCStoreFromDisk(t, m.store).Read()
			if scenario == "success" {
				if reopened.RPCState.ExitProtection.LAN != nil || reopened.RPCState.ExitChange.Protection.LAN != nil {
					t.Fatal("journal survived confirmed cleanup")
				}
			} else if reopened.RPCState.ExitProtection.LAN == nil || reopened.RPCState.ExitChange.Protection.LAN == nil {
				t.Fatal("failed cleanup lost recovery intent")
			}
			if !reflect.DeepEqual(scope.LAN, manifest) {
				t.Fatal("input ownership mutated")
			}
		})
	}
}
