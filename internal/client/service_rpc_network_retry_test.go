package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNetworkDispatchRetriesCompensationAcrossRestart(t *testing.T) {
	for _, code := range []ipc.ErrorCode{ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED} {
		for _, sourceChanged := range []bool{false, true} {
			t.Run(code.String()+map[bool]string{false: "/registration", true: "/source_changed"}[sourceChanged], func(t *testing.T) {
				m, _ := networkRegistrationExecutorFixture(t)
				id := m.store.Read().RPCState.NetworkSelection.OperationID
				if sourceChanged {
					if err := m.store.Update(func(cfg *Config) error { cfg.Token = "new-synthetic-session"; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				registrations, cleanups := 0, 0
				retry := true
				providers := ClientRPCNetworkSelectionProviders{
					Register: func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) (*ipc.UserAction, error) {
						registrations++
						return nil, rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
					},
					Cleanup: func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error {
						cleanups++
						if retry {
							return rpc.Error(connect.CodeUnavailable, code)
						}
						return nil
					},
				}
				driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					t.Error("compensation before teardown stopped the active source")
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED, nil
				}}
				for attempt := 0; attempt < 2; attempt++ {
					if err := m.ReconcileNetworkSelection(t.Context(), driver, providers); err != nil {
						t.Fatal("temporary compensation failure stopped dispatcher", err)
					}
					plan := m.store.Read().RPCState.NetworkSelection
					if plan == nil || plan.AbortFailure == nil || plan.TargetRevoked || rpcOperationTerminal(networkApplyOperation(t, m, id).State) {
						t.Fatal("uncertain remote cleanup was discarded or completed")
					}
					var err error
					m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
					if err != nil {
						t.Fatal(err)
					}
				}
				retry = false
				if err := m.ReconcileNetworkSelection(t.Context(), driver, providers); err != nil {
					t.Fatal(err)
				}
				wantCode, wantRegistrations := ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, 1
				if sourceChanged {
					wantCode, wantRegistrations = ipc.ErrorCode_ERROR_CODE_STALE_STATE, 0
				}
				result := networkApplyOperation(t, m, id)
				if result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != wantCode || m.store.Read().RPCState.NetworkSelection != nil || cleanups != 3 || registrations != wantRegistrations {
					t.Fatal("compensation retry lost its original failure or repeated registration")
				}
			})
		}
	}
}

func TestNetworkDispatchRetriesApplyCleanupWithoutReapplying(t *testing.T) {
	for _, code := range []ipc.ErrorCode{ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED} {
		t.Run(code.String(), func(t *testing.T) {
			m, _ := readyNetworkActivationFixture(t)
			id := m.store.Read().RPCState.NetworkSelection.OperationID
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
				cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			starts, stops := 0, 0
			retry := false
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
				starts++
				return errors.New("synthetic partial apply")
			}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				if retry {
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeUnavailable, code)
				}
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}}
			if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			retry = true
			for attempt := 0; attempt < 2; attempt++ {
				if err := m.ReconcileNetworkSelection(t.Context(), driver, ClientRPCNetworkSelectionProviders{}); err != nil {
					t.Fatal("temporary local cleanup stopped dispatcher", err)
				}
				plan := m.store.Read().RPCState.NetworkSelection
				if plan == nil || plan.ApplyFailure == nil || !plan.DownStarted || rpcOperationTerminal(networkApplyOperation(t, m, id).State) {
					t.Fatal("uncertain local cleanup lost its barrier")
				}
				var err error
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
			}
			retry = false
			if err := m.ReconcileNetworkSelection(t.Context(), driver, ClientRPCNetworkSelectionProviders{}); err != nil {
				t.Fatal(err)
			}
			result := networkApplyOperation(t, m, id)
			if result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || starts != 1 || stops != 4 || m.store.Read().RPCState.NetworkSelection != nil {
				t.Fatal("cleanup retry repeated apply or completed before teardown")
			}
		})
	}
}
