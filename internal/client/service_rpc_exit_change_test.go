package client

import (
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCExitSelectionAdmissionBindingAndRestart(t *testing.T) {
	for _, scenario := range []string{"valid", "observer", "expired", "tampered", "stale_map", "unsupported_pair", "policy_denied", "clear_offline"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			if scenario == "expired" {
				networkMap.Network.ClientPolicy.ExitNodes[0].ExpiresAt = now.Add(-time.Second)
			}
			resignApplicationMap(t, &networkMap, key)
			if scenario == "tampered" {
				networkMap.Network.ClientPolicy.ExitNodes[0].Name = "tampered"
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				if scenario == "stale_map" {
					cfg.MapGlobalRevision++
				}
				if scenario == "clear_offline" {
					cfg.ExitSelection = &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
					cfg.CachedMap = nil
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			supported := []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}, {Family: api.ExitFamilyIPv6Only, LAN: api.ExitLANAllow}}
			request := &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}
			if scenario == "unsupported_pair" || scenario == "policy_denied" {
				request.LanAccess = ipc.LanAccess_LAN_ACCESS_ALLOW
			}
			if scenario == "policy_denied" {
				supported = append(supported, clientRPCExitMode{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANAllow})
			}
			caller := owner
			if scenario == "observer" {
				caller = local.Peer{Identity: "uid:other"}
			}
			before := m.store.Read()
			var op *ipc.Operation
			var err error
			clear := &ipc.ClearExitNodeRequest{Mutation: request.Mutation, Profile: profile}
			if scenario == "clear_offline" {
				op, err = m.clearExitNodeAs(caller, clear)
			} else {
				op, err = m.selectExitNodeAs(caller, request, supported)
			}
			if scenario != "valid" && scenario != "clear_offline" {
				if err == nil || !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("invalid exit admission changed durable state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if op.State != ipc.OperationState_OPERATION_STATE_PENDING || !reflect.DeepEqual(before.ExitSelection, m.store.Read().ExitSelection) {
				t.Fatal("admission applied routing selection")
			}
			plan := m.store.Read().RPCState.ExitChange
			if plan == nil || plan.OperationID != op.Id || plan.ProfileID != profile.ProfileId || plan.OwnerID != owner.Identity {
				t.Fatal("exit operation lost binding")
			}
			if scenario == "valid" && (plan.Requested == nil || plan.Requested.Host != host || plan.Requested.Family != api.ExitFamilyIPv4Only) {
				t.Fatal("grant identity not pinned")
			}
			if scenario == "clear_offline" && (plan.Requested != nil || plan.Previous == nil) {
				t.Fatal("clear lost previous selection")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(plan, m.store.Read().RPCState.ExitChange) {
				t.Fatal("restart changed exit intent")
			}
			if scenario == "clear_offline" {
				replay, err := m.clearExitNodeAs(owner, clear)
				if err != nil || !proto.Equal(op, replay) {
					t.Fatal("clear replay changed", err)
				}
			} else {
				replay, err := m.selectExitNodeAs(owner, request, nil)
				if err != nil || !proto.Equal(op, replay) {
					t.Fatal("accepted selection replay reevaluated support", err)
				}
			}
			_, err = m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
		})
	}
}

func TestNodeCleanupRemovesBoundExitSelection(t *testing.T) {
	for _, logout := range []bool{false, true} {
		cfg := Config{NodeID: "node", NetworkID: "network", ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
		if logout {
			if err := ApplyLocalLogoutCleanup(&cfg, time.Now()); err != nil {
				t.Fatal(err)
			}
		} else {
			clearNodeBoundState(&cfg)
		}
		if cfg.ExitSelection != nil || cfg.NodeID != "" || cfg.NetworkID != "" {
			t.Fatal("node-bound selection survived cleanup")
		}
	}
}
