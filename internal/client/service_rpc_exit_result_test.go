package client

import (
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func appliedExitTestStatus(profile string, selected bool) *ipc.ExitNodeStatus {
	status := &ipc.ExitNodeStatus{ProfileId: profile, ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, RequestedFamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_NONE, Ipv4: &ipc.ExitFamilyStatus{ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED}, Ipv6: &ipc.ExitFamilyStatus{ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED}}
	if selected {
		id := "exit"
		status.RequestedExitNodeId, status.EffectiveExitNodeId = &id, &id
		status.RequestedFamilyMode = ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY
		status.RequestedLanAccess, status.EffectiveLanAccess = ipc.LanAccess_LAN_ACCESS_BLOCK, ipc.LanAccess_LAN_ACCESS_BLOCK
		status.FailClosed = true
		status.Ipv4.RequestedExitNodeId, status.Ipv4.EffectiveExitNodeId, status.Ipv4.FailClosed = &id, &id, true
	}
	return status
}

func TestRPCExitResultRejectsPartialOrStaleApplication(t *testing.T) {
	for _, scenario := range []string{"select", "clear", "partial", "unprotected", "wrong_lan", "missing_family", "unexpected_ipv6", "node_changed", "selection_changed", "expired_grant"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			m.now = func() time.Time { return now }
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &networkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				if scenario == "clear" {
					cfg.ExitSelection = &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
					cfg.CachedMap = nil
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}
			clear := &ipc.ClearExitNodeRequest{Mutation: request.Mutation, Profile: profile}
			var op *ipc.Operation
			var err error
			if scenario == "clear" {
				op, err = m.clearExitNodeAs(owner, clear)
			} else {
				op, err = m.selectExitNodeAs(owner, request, []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
				op.State = ipc.OperationState_OPERATION_STATE_RUNNING
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			status := appliedExitTestStatus(profile.ProfileId, scenario != "clear")
			switch scenario {
			case "partial":
				status.Ipv4.ApplyState = ipc.ApplyState_APPLY_STATE_PENDING
			case "unprotected":
				status.Ipv4.FailClosed = false
			case "wrong_lan":
				status.EffectiveLanAccess = ipc.LanAccess_LAN_ACCESS_ALLOW
			case "missing_family":
				status.Ipv6 = nil
			case "unexpected_ipv6":
				status.Ipv6.EffectiveExitNodeId = proto.String("exit")
			case "expired_grant":
				now = now.Add(2 * time.Minute)
			case "node_changed", "selection_changed":
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "node_changed" {
						cfg.NodeID = "other"
					} else {
						cfg.ExitSelection = &ClientExitSelection{ID: "other"}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.store.Read()
			completed, err := m.completeExitChange(op.Id, status, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED)
			if scenario != "select" && scenario != "clear" {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("rejected result partially committed selection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if cfg.RPCState.ExitChange != nil || completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.NodeID != before.NodeID || !reflect.DeepEqual(cfg.ConnectionIntent, before.ConnectionIntent) {
				t.Fatal("completion lost atomicity or changed unrelated context")
			}
			if scenario == "select" && (cfg.ExitSelection == nil || completed.GetSelection().SelectedId != "exit") {
				t.Fatal("selection not committed")
			}
			if scenario == "clear" && (cfg.ExitSelection != nil || completed.GetSelection().SelectedId != "") {
				t.Fatal("clear not committed")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			var replay *ipc.Operation
			if scenario == "clear" {
				replay, err = m.clearExitNodeAs(owner, clear)
			} else {
				replay, err = m.selectExitNodeAs(owner, request, nil)
			}
			if err != nil || !proto.Equal(completed, replay) {
				t.Fatal("completed exit replay changed after restart", err)
			}
		})
	}
}
