package client

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func networkSelectionPlanFixture(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.SelectNetworkRequest) {
	t.Helper()
	m, owner, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ControlPlaneURLs = []string{cfg.RPCState.Profiles[profile.ProfileId].ControlOrigin}
		cfg.Token, cfg.ActiveAccountID, cfg.NodeCredential = "synthetic-private-session", "account", "synthetic-private-node"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, owner, &ipc.SelectNetworkRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, NetworkId: "target"}
}

func TestNetworkSelectionPreparationPersistsIsolatedPlanAndReplay(t *testing.T) {
	m, owner, request := networkSelectionPlanFixture(t)
	before := networkSelectionContext(m.store.Read())
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != ipc.OperationState_OPERATION_STATE_PENDING || !reflect.DeepEqual(before, networkSelectionContext(m.store.Read())) {
		t.Fatal("admission changed live network or completed selection")
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil || !proto.Equal(replay, op) {
		t.Fatal("admission replay changed operation", err)
	}
	calls := 0
	provider := func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		calls++
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	prepared := m.store.Read()
	if prepared.RPCState.NetworkSelection.Target == nil || prepared.RPCState.NetworkSelection.Target.NodeCredential != "" || !reflect.DeepEqual(before, networkSelectionContext(prepared)) {
		t.Fatal("prepared candidate mixed or activated network authority")
	}
	store = reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), provider); err != nil || calls != 1 {
		t.Fatal("checkpoint recovery repeated catalog preparation", err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protojson.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || strings.Contains(string(raw), "synthetic-private") {
		t.Fatal("preparation leaked authority or claimed completion")
	}
}

func TestNetworkSelectionPreparationRejectsConcurrentSourceChange(t *testing.T) {
	for _, change := range []string{"intent", "owner", "profile"} {
		t.Run(change, func(t *testing.T) {
			m, owner, request := networkSelectionPlanFixture(t)
			op, err := m.beginNetworkSelectionAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			err = m.ReconcileNetworkSelectionPreparation(t.Context(), func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
				if err := m.store.Update(func(cfg *Config) error {
					switch change {
					case "intent":
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
					case "owner":
						cfg.LocalOwnerID = "another-owner"
					case "profile":
						p := cfg.RPCState.Profiles[request.Profile.ProfileId]
						p.DisplayName = "renamed"
						cfg.RPCState.Profiles[p.ID] = p
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
			})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			if cfg := m.store.Read(); cfg.RPCState.NetworkSelection.Target != nil || cfg.RPCState.Revision != op.Metadata.Revision {
				t.Fatal("stale provider committed target or advanced journal")
			}
		})
	}
}
