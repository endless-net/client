package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitSelectionBindsSignedMapAcrossRestartAndApply(t *testing.T) {
	for _, duringApply := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "dispatched"}[duringApply], func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{
				ID: "exit", Name: "Exit", Host: api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey},
				ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock},
			}}}
			resignApplicationMap(t, &networkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			modes := []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}}
			op, err := m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, modes)
			if err != nil {
				t.Fatal(err)
			}
			originalHash := networkMap.MapSignature.PayloadHash
			replaceMap := func() {
				t.Helper()
				// A valid replacement retaining the recipient, revisions and grant
				// still cannot attest application of the originally admitted map.
				networkMap.Network.ClientPolicy.ExitNodes[0].Name = "Updated exit"
				resignApplicationMap(t, &networkMap, key)
				if networkMap.MapSignature.PayloadHash == originalHash {
					t.Fatal("fixture did not replace the signed payload")
				}
				if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap = &networkMap; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if !duringApply {
				replaceMap()
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if m.store.Read().RPCState.ExitChange.MapHash != originalHash {
				t.Fatal("restart lost admitted map binding")
			}
			applies, contains := 0, 0
			executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{}, Modes: modes, Apply: func(_ context.Context, _ string, input Config, _ *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
				applies++
				if input.CachedMap.MapSignature.PayloadHash != originalHash {
					t.Fatal("replacement map reached executor")
				}
				replaceMap()
				return appliedExitTestStatus(profile.ProfileId, true), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}, Contain: func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
				contains++
				if plan.MapHash != originalHash || !plan.Containing {
					t.Fatal("containment lost original apply context")
				}
				return clientRPCExitContainment{}, errors.New("injected containment failure")
			}}
			err = m.reconcileExitChange(t.Context(), executor)
			if duringApply {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				executor.Apply = nil
				executor.Contain = func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
					contains++
					return clientRPCExitContainment{OperationID: plan.OperationID, ProfileID: plan.ProfileID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, IPv4Blocked: true, IPv6Blocked: true, ExitRoutesRemoved: true}, nil
				}
				err = m.reconcileExitChange(t.Context(), executor)
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.GetState() != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE || m.store.Read().ExitSelection != nil || m.store.Read().RPCState.ExitChange != nil {
				t.Fatal("stale map result committed selection or retained completed guard", err, result)
			}
			if (duringApply && (applies != 1 || contains != 2)) || (!duringApply && (applies != 0 || contains != 0)) {
				t.Fatal("unexpected dispatch or containment", applies, contains)
			}
		})
	}
}
