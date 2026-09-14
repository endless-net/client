package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestPreferenceAndResourceCleanupRequiresConfirmedStop(t *testing.T) {
	for _, resource := range []bool{false, true} {
		for _, connected := range []bool{false, true} {
			for _, continuity := range []ipc.ConnectionContinuity{ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNSPECIFIED, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED, 777} {
				t.Run(map[bool]string{false: "preferences", true: "resource"}[resource]+"/"+map[bool]string{false: "disconnected", true: "connected"}[connected]+"/"+continuity.String(), func(t *testing.T) {
					m, owner, profile := rpcPreferenceFixture(t)
					if err := m.store.Update(func(cfg *Config) error {
						desired := ConnectionIntentDesiredDisconnected
						if connected {
							desired = ConnectionIntentDesiredConnected
						}
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: desired}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					var op *ipc.Operation
					var err error
					if resource {
						id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
						op, err = m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id, Enabled: false})
					} else {
						op, err = m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
					}
					if err != nil {
						t.Fatal(err)
					}
					starts, stops := 0, 0
					driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
						starts++
						return errors.New("synthetic apply failure")
					}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) { stops++; return continuity, nil }}
					err = m.ReconcileNetworkPreferences(t.Context(), driver)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
					cfg := m.store.Read()
					if cfg.RPCState.NetworkPreferenceChange == nil || !cfg.RPCState.NetworkPreferenceChange.Containing || cfg.ResourcePreferences != nil || cfg.NetworkPreferences != nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
						t.Fatal("unconfirmed stop committed or released containment")
					}
					pending, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
					if err != nil || pending.State != ipc.OperationState_OPERATION_STATE_RUNNING {
						t.Fatal("unconfirmed cleanup became terminal")
					}
					m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
					if err != nil {
						t.Fatal(err)
					}
					beforeStarts, beforeStops := starts, stops
					driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
						stops++
						return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
					}
					if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
						t.Fatal(err)
					}
					result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
					if err != nil || result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || m.store.Read().RPCState.NetworkPreferenceChange != nil {
						t.Fatal("confirmed cleanup did not preserve original failure")
					}
					if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
						t.Fatal(err)
					}
					if starts != beforeStarts || stops != beforeStops+1 || (connected && starts != 1) || (!connected && starts != 0) {
						t.Fatal("cleanup recovery repeated apply or completed effects")
					}
				})
			}
		}
	}
}
