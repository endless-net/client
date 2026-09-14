package client

import (
	"context"
	"reflect"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNetworkSelectionPreservesBoundRequestedIntentAcrossActivationRestart(t *testing.T) {
	for _, mode := range []string{"bound", "unbound", "user_disconnect"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := readyNetworkActivationFixture(t)
			original := &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect", UpdatedAt: "2026-09-14T00:00:00Z"}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "runtime_start_policy_unavailable", StartupRecovery: &clientRuntimeStartRecovery{Context: runtimeStartRecoveryContext(*cfg), PreviousState: original.DesiredState, PreviousReason: original.Reason, PreviousUpdatedAt: original.UpdatedAt}}
				if mode == "unbound" {
					cfg.ConnectionIntent.StartupRecovery.Context[0] ^= 1
				}
				if mode == "user_disconnect" {
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
				}
				cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			source := m.store.Read()
			id := source.RPCState.NetworkSelection.OperationID
			starts, stops := 0, 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
			}, Start: func(_ context.Context, cfg Config) error {
				starts++
				if mode != "bound" || cfg.NetworkID == source.NetworkID || cfg.NodeID == source.NodeID || !networkSelectionTargetReady(cfg, m.now()) || cfg.ConnectionIntent.StartupRecovery != nil {
					t.Fatal("unbound or unverified target started")
				}
				return nil
			}}
			if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			activated := m.store.Read()
			if starts != 0 || stops != 1 || activated.ConnectionIntent.StartupRecovery != nil {
				t.Fatal("activation applied early or retained source checkpoint")
			}
			if mode == "bound" && !reflect.DeepEqual(activated.ConnectionIntent, original) {
				t.Fatal("activation lost requested connectivity")
			}
			if mode != "bound" && activated.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("activation revived superseded intent")
			}
			store := reopenRPCStoreFromDisk(t, m.store)
			var err error
			m, err = NewClientRPCMutations(store)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			if op := networkApplyOperation(t, m, id); op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatal("selection failed after durable activation")
			}
			if (mode == "bound" && starts != 1) || (mode != "bound" && starts != 0) {
				t.Fatal("wrong target connectivity")
			}
			if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			if (mode == "bound" && (starts != 1 || stops != 1)) || (mode != "bound" && (starts != 0 || stops != 2)) {
				t.Fatal("completed selection repeated effects")
			}
		})
	}
}
