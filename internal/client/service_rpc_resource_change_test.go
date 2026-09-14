package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestResourceChangeAdmissionAndRestart(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
	request := &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id, Enabled: false}
	op, err := m.setResourceEnabledAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	plan := cfg.RPCState.NetworkPreferenceChange
	if op.Kind != ipc.OperationKind_OPERATION_KIND_SET_RESOURCE_ENABLED || op.State != ipc.OperationState_OPERATION_STATE_PENDING || cfg.ResourcePreferences != nil || plan == nil || plan.ResourceID != id || !plan.Changed {
		t.Fatal("admission did not retain unapplied resource intent")
	}
	if enabled, exists := plan.RequestedResources[id]; !exists || enabled {
		t.Fatal("explicit false lost")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan, m.store.Read().RPCState.NetworkPreferenceChange) {
		t.Fatal("restart lost plan")
	}
	replay, err := m.setResourceEnabledAs(owner, request)
	if err != nil || !proto.Equal(op, replay) {
		t.Fatal("replay lost", err)
	}
	_, err = m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
}

func TestResourceChangeRejectsAtomically(t *testing.T) {
	for _, scenario := range []string{"unknown", "locked", "tampered", "observer"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			opts, key := signedServiceDNSFixture(t)
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, opts.NetworkMap.Peers[0].ID)
			want := ipc.ErrorCode_ERROR_CODE_NOT_FOUND
			switch scenario {
			case "unknown":
				id = rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, "missing")
			case "locked":
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceHost, ID: opts.NetworkMap.Peers[0].ID, Source: api.ClientPolicyDevice, PolicyID: "host", Locked: true, Enabled: true}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
				want = ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED
			case "tampered":
				opts.NetworkMap.Network.Name = "tampered"
				want = ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			case "observer":
				owner.Identity = "uid:other"
				want = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap = &opts.NetworkMap
				cfg.MapSigningTrust = opts.SigningTrust
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			_, err := m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id})
			assertRPCFailure(t, err, want)
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("rejected resource changed state")
			}
		})
	}
}

func TestResourceWorkerAppliesOrContains(t *testing.T) {
	for _, scenario := range []string{"apply", "failure", "disconnect", "resource_changed"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id})
			if err != nil {
				t.Fatal(err)
			}
			stops := 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(_ context.Context, cfg Config) error {
				if value, exists := cfg.ResourcePreferences[id]; !exists || value {
					t.Fatal("driver lost candidate")
				}
				if m.store.Read().ResourcePreferences != nil {
					t.Fatal("published before apply")
				}
				switch scenario {
				case "failure":
					return errors.New("apply failure")
				case "disconnect":
					_, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
					if err != nil {
						t.Fatal(err)
					}
				case "resource_changed":
					if err := m.store.Update(func(cfg *Config) error { cfg.ResourcePreferences = map[string]bool{id: true}; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}}
			if err := m.ReconcileNetworkPreferences(context.Background(), driver); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if cfg.RPCState.NetworkPreferenceChange != nil {
				t.Fatal("terminal plan retained")
			}
			if scenario == "apply" {
				value, exists := cfg.ResourcePreferences[id]
				if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !exists || value || stops != 0 {
					t.Fatal("applied resource not committed", result)
				}
				return
			}
			reason := "resource_apply_failed"
			state := ipc.OperationState_OPERATION_STATE_FAILED
			if scenario == "disconnect" {
				reason = "resource_superseded_by_disconnect"
				state = ipc.OperationState_OPERATION_STATE_CANCELLED
			}
			if scenario == "resource_changed" {
				reason = "resource_context_changed"
			}
			if result.State != state || result.GetFailure().GetReasonKey() != reason || stops != 1 || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("containment lost cause", result)
			}
			if scenario == "resource_changed" {
				if !cfg.ResourcePreferences[id] {
					t.Fatal("overwrote concurrent resource choice")
				}
			} else if cfg.ResourcePreferences != nil {
				t.Fatal("failed candidate committed")
			}
		})
	}
}
