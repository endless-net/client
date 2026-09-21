package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNetworkPreferenceWorkerApplyAndContainment(t *testing.T) {
	for _, scenario := range []string{"apply", "disconnected", "failure", "disconnect_during_apply", "map_changed_during_apply", "tampered_before_apply", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				if scenario == "disconnected" {
					cfg.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), AcceptRoutes: proto.Bool(false), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), RuntimeStart: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum(), UserLogoff: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Suspend: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum(), Resume: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT.Enum()}})
			if err != nil {
				t.Fatal(err)
			}
			response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			for _, setting := range []*ipc.LifecycleSetting{response.Msg.Preferences.Lifecycle.UserLogoff, response.Msg.Preferences.Lifecycle.Suspend, response.Msg.Preferences.Lifecycle.Resume} {
				if setting.Requested == nil || setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT || setting.Control.Mutation.ReasonKey != "preference_patch_pending" {
					t.Fatal("pending desktop projection lost", setting)
				}
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "tampered_before_apply" {
				if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Name = "tampered"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			starts, stops := 0, 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(ctx context.Context, cfg Config) error {
				starts++
				if cfg.NetworkPreferences == nil || cfg.NetworkPreferences.AcceptDNS == nil || *cfg.NetworkPreferences.AcceptDNS || *cfg.NetworkPreferences.AcceptRoutes {
					t.Fatal("driver did not receive candidate")
				}
				if m.store.Read().NetworkPreferences != nil || m.store.Read().RPCState.Profiles[profile.ProfileId].RuntimeStart != nil {
					t.Fatal("candidate published before effect")
				}
				switch scenario {
				case "failure":
					return errors.New("apply failed")
				case "shutdown":
					cancel()
					return ctx.Err()
				case "disconnect_during_apply":
					_, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
					if err != nil {
						t.Fatal(err)
					}
				case "map_changed_during_apply":
					if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Name = "tampered"; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}}
			err = m.ReconcileNetworkPreferences(ctx, driver)
			if scenario == "shutdown" {
				if !errors.Is(err, context.Canceled) || m.store.Read().RPCState.NetworkPreferenceChange == nil || stops != 1 {
					t.Fatal("shutdown lost resumable operation", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			committed := cfg.RPCState.Profiles[profile.ProfileId]
			for i, value := range []*ipc.LifecycleBehavior{committed.UserLogoff, committed.Suspend, committed.Resume} {
				if scenario == "apply" || scenario == "disconnected" {
					want := ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT
					if i == 2 {
						want = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
					}
					if value == nil || *value != want {
						t.Fatal("successful effect lost desktop preference")
					}
				} else if value != nil {
					t.Fatal("failed effect partially committed desktop preference")
				}
			}
			if cfg.RPCState.NetworkPreferenceChange != nil {
				t.Fatal("terminal operation retained plan")
			}
			if scenario == "apply" || scenario == "disconnected" {
				if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.NetworkPreferences == nil || cfg.RPCState.Profiles[profile.ProfileId].UIQuit == nil || cfg.RPCState.Profiles[profile.ProfileId].RuntimeStart == nil || *cfg.RPCState.Profiles[profile.ProfileId].RuntimeStart != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT {
					t.Fatal("successful effect did not commit atomic patch")
				}
			} else {
				wantState, wantCode := ipc.OperationState_OPERATION_STATE_FAILED, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED
				if scenario == "disconnect_during_apply" {
					wantState, wantCode = ipc.OperationState_OPERATION_STATE_CANCELLED, ipc.ErrorCode_ERROR_CODE_CANCELLED
					if cfg.ConnectionIntent.Reason != "user_disconnect" {
						t.Fatal("containment overwrote accepted disconnect reason")
					}
				}
				if scenario == "map_changed_during_apply" {
					wantCode = ipc.ErrorCode_ERROR_CODE_STALE_STATE
				}
				if scenario == "tampered_before_apply" {
					wantCode = ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
				}
				if result.State != wantState || result.GetFailure().GetCode() != wantCode || cfg.NetworkPreferences != nil || cfg.RPCState.Profiles[profile.ProfileId].UIQuit != nil || cfg.RPCState.Profiles[profile.ProfileId].RuntimeStart != nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || stops != 1 {
					t.Fatal("failure cause or containment lost", result)
				}
			}
			if (scenario == "tampered_before_apply" || scenario == "disconnected") && starts != 0 {
				t.Fatal("worker started unauthorized or disconnected candidate")
			}
		})
	}
}

func TestNetworkPreferenceContainmentRecoversAfterDownFailure(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
	if err != nil {
		t.Fatal(err)
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return errors.New("apply failed") }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("down failed")
	}}
	if err := m.ReconcileNetworkPreferences(context.Background(), driver); err == nil {
		t.Fatal("down failure hidden")
	}
	cfg := m.store.Read()
	if cfg.RPCState.NetworkPreferenceChange == nil || !cfg.RPCState.NetworkPreferenceChange.Containing || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("containment was not durable before Down")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	driver.Start = func(context.Context, Config) error {
		t.Fatal("restarted containment retried failed candidate")
		return nil
	}
	driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}
	if err := m.ReconcileNetworkPreferences(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	if m.store.Read().RPCState.NetworkPreferenceChange != nil || m.store.Read().NetworkPreferences != nil {
		t.Fatal("recovered containment applied candidate or retained plan")
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || result.GetFailure().GetReasonKey() != "preferences_apply_failed" {
		t.Fatal("restart lost original failure cause", err, result)
	}
}
