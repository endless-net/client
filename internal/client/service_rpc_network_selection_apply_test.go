package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func activatedNetworkApplyFixture(t *testing.T, connected bool) *ClientRPCMutations {
	t.Helper()
	m, _ := readyNetworkActivationFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		desired := ConnectionIntentDesiredDisconnected
		if connected {
			desired = ConnectionIntentDesiredConnected
		}
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: desired, Reason: "synthetic_user_intent"}
		cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionActivation(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		// Native Down can confirm removal without knowing prior continuity.
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}); err != nil {
		t.Fatal(err)
	}
	return m
}

func networkApplyOperation(t *testing.T, m *ClientRPCMutations, id string) *ipc.Operation {
	t.Helper()
	for _, record := range m.store.Read().RPCState.Operations {
		op := new(ipc.Operation)
		if err := proto.Unmarshal(record.Operation, op); err != nil {
			t.Fatal(err)
		}
		if op.Id == id {
			return op
		}
	}
	t.Fatal("operation missing")
	return nil
}

func TestNetworkApplyCompletesOnlyCurrentTargetAndReplays(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(map[bool]string{true: "connected", false: "disconnected"}[connected], func(t *testing.T) {
			m := activatedNetworkApplyFixture(t, connected)
			id := m.store.Read().RPCState.NetworkSelection.OperationID
			target := networkSelectionContext(m.store.Read())
			starts, stops := 0, 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}, Start: func(_ context.Context, cfg Config) error {
				starts++
				if !reflect.DeepEqual(networkSelectionContext(cfg), target) || !m.store.Read().RPCState.NetworkSelection.ApplyStarted || networkApplyOperation(t, m, id).State != ipc.OperationState_OPERATION_STATE_RUNNING {
					t.Fatal("apply lost target, durable marker or claimed early completion")
				}
				return nil
			}}
			if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			if m.store.Read().RPCState.NetworkSelection != nil || !reflect.DeepEqual(networkSelectionContext(m.store.Read()), target) {
				t.Fatal("selection retained barrier or changed target")
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
			op := networkApplyOperation(t, m, id)
			if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetSelection().SelectedId != "target" || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN ||
				(connected && (starts != 1 || stops != 0)) || (!connected && (starts != 0 || stops != 1)) {
				t.Fatal("incorrect completion, connectivity or repeated apply", op)
			}
		})
	}
}

func TestNetworkApplyFailureResumesCleanupWithoutReapply(t *testing.T) {
	m := activatedNetworkApplyFixture(t, true)
	id := m.store.Read().RPCState.NetworkSelection.OperationID
	starts, stops := 0, 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
		starts++
		return errors.New("synthetic-private-provider-detail")
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		if stops == 1 {
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("synthetic partial cleanup")
		}
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	plan := m.store.Read().RPCState.NetworkSelection
	if plan == nil || plan.ApplyFailure == nil || !plan.DownStarted || networkApplyOperation(t, m, id).State != ipc.OperationState_OPERATION_STATE_RUNNING {
		t.Fatal("partial cleanup released barrier or reported terminal failure")
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
	op := networkApplyOperation(t, m, id)
	if starts != 1 || stops != 2 || m.store.Read().RPCState.NetworkSelection != nil || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected ||
		op.State != ipc.OperationState_OPERATION_STATE_FAILED || op.GetFailure().ReasonKey != "network_selection_apply_failed" {
		t.Fatal("cleanup replay applied again, lost failure or retained connected intent", op)
	}
}

func TestNetworkApplyContainsContextChangeDespiteProviderSuccess(t *testing.T) {
	for _, mode := range []string{"owner", "disconnect", "map"} {
		t.Run(mode, func(t *testing.T) {
			m := activatedNetworkApplyFixture(t, true)
			id := m.store.Read().RPCState.NetworkSelection.OperationID
			stops := 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(applyCtx context.Context, _ Config) error {
				if mode == "disconnect" {
					cfg := m.store.Read()
					_, err := m.disconnectAs(local.Peer{Identity: cfg.LocalOwnerID}, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: cfg.RPCState.ActiveProfileID}})
					if err != nil || !errors.Is(applyCtx.Err(), context.Canceled) {
						t.Fatal("accepted disconnect did not cancel in-flight target apply", err)
					}
					return nil // Even a provider ignoring cancellation cannot commit success.
				}
				return m.store.Update(func(cfg *Config) error {
					switch mode {
					case "owner":
						cfg.LocalOwnerID = "replacement-owner"
					case "map":
						cfg.CachedMap.Network.Name = "tampered"
					}
					return nil
				})
			}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}}
			if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			wantState := ipc.OperationState_OPERATION_STATE_FAILED
			if mode == "disconnect" {
				wantState = ipc.OperationState_OPERATION_STATE_CANCELLED
			}
			if stops != 1 || networkApplyOperation(t, m, id).State != wantState {
				t.Fatal("stale successful provider result escaped cleanup")
			}
			if mode == "disconnect" && m.store.Read().ConnectionIntent.Reason != "user_disconnect" {
				t.Fatal("newer disconnect overwritten")
			}
		})
	}
}

func TestNetworkApplyRecoversUncertainAttemptByStoppingFirst(t *testing.T) {
	m := activatedNetworkApplyFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	starts, stops := 0, 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
		starts++
		if starts == 1 {
			cancel()
		} else if stops != 1 {
			t.Fatal("recovery reapplied without stopping uncertain target")
		}
		return nil
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkSelectionApply(ctx, driver); !errors.Is(err, context.Canceled) {
		t.Fatal("lost runtime cancellation", err)
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionApply(t.Context(), driver); err != nil || starts != 2 || stops != 1 {
		t.Fatal("uncertain apply did not recover", err)
	}
}
