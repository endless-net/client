package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestResourceWorkerRestartsApplyOrContainment(t *testing.T) {
	for _, scenario := range []string{"shutdown", "commit_cancel", "apply_failed", "disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			interrupted := scenario == "shutdown" || scenario == "commit_cancel"
			m, owner, profile := rpcPreferenceFixture(t)
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ResourcePreferences = map[string]bool{id: true}
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id}
			accepted, err := m.setResourceEnabledAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			starts, stops := 0, 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(ctx context.Context, cfg Config) error {
				starts++
				if value, exists := cfg.ResourcePreferences[id]; !exists || value {
					t.Fatal("resumed candidate lost explicit false")
				}
				if scenario == "shutdown" {
					cancel()
					return ctx.Err()
				}
				if scenario == "commit_cancel" {
					m.now = func() time.Time { cancel(); return time.Now() }
					return nil
				}
				if scenario == "disconnect" {
					if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
						t.Fatal(err)
					}
				}
				return errors.New("apply failed")
			}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("down failed")
			}}
			if err := m.ReconcileNetworkPreferences(ctx, driver); err == nil {
				t.Fatal("interrupted recovery falsely completed")
			}
			before := m.store.Read()
			plan := before.RPCState.NetworkPreferenceChange
			if plan == nil || plan.Containing != !interrupted || !before.ResourcePreferences[id] {
				t.Fatal("restart state lost prior choice or recovery phase")
			}
			if !interrupted && before.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("down failure did not persist disconnected intent")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			driver.Start = func(_ context.Context, cfg Config) error {
				starts++
				if !interrupted {
					t.Fatal("containment restarted failed candidate")
				}
				if value, exists := cfg.ResourcePreferences[id]; !exists || value {
					t.Fatal("resumed candidate lost false")
				}
				return nil
			}
			driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}
			if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: accepted.Id}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if cfg.RPCState.NetworkPreferenceChange != nil {
				t.Fatal("terminal recovery retained plan")
			}
			if interrupted {
				if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.ResourcePreferences[id] || starts != 2 || stops != 0 {
					t.Fatal("restart did not finish pending apply", result)
				}
			} else {
				wantState, wantCode, wantReason := ipc.OperationState_OPERATION_STATE_FAILED, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, "resource_apply_failed"
				if scenario == "disconnect" {
					wantState, wantCode, wantReason = ipc.OperationState_OPERATION_STATE_CANCELLED, ipc.ErrorCode_ERROR_CODE_CANCELLED, "resource_superseded_by_disconnect"
					if cfg.ConnectionIntent.Reason != "user_disconnect" {
						t.Fatal("recovery replaced user disconnect")
					}
				}
				if result.State != wantState || result.GetFailure().GetCode() != wantCode || result.GetFailure().GetReasonKey() != wantReason || !cfg.ResourcePreferences[id] || starts != 1 || stops != 2 {
					t.Fatal("containment lost prior choice or original cause", result)
				}
			}
			replay, err := m.setResourceEnabledAs(owner, request)
			if err != nil || !proto.Equal(replay, result) {
				t.Fatal("replay after recovery lost terminal result", err)
			}
		})
	}
}
