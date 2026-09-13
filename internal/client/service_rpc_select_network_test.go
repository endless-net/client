package client

import (
	"reflect"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCSelectCurrentNetworkIsDurableNoop(t *testing.T) {
	for _, mode := range []string{"connected", "disconnected", "foreign", "name", "empty", "observer", "unenrolled", "pending"} {
		t.Run(mode, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NetworkID, cfg.NodeCredential = "network-id", "synthetic-credential"
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				if mode == "disconnected" {
					cfg.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
				}
				if mode == "unenrolled" {
					cfg.NodeCredential = ""
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "pending" {
				if _, err := m.connectAs(owner, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
					t.Fatal(err)
				}
			}
			request := &ipc.SelectNetworkRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, NetworkId: "network-id"}
			peer := owner
			want := ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
			switch mode {
			case "foreign":
				request.NetworkId, want = "other", ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "name":
				request.NetworkId, want = "Network Name", ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "empty":
				request.NetworkId, want = "", ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
			case "observer":
				peer, want = local.Peer{Identity: "other-owner"}, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			case "unenrolled":
				want = ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT
			case "pending":
				want = ipc.ErrorCode_ERROR_CODE_BUSY
			}
			before := m.store.Read()
			op, err := m.selectNetworkAs(peer, request)
			if want != ipc.ErrorCode_ERROR_CODE_UNSPECIFIED {
				if rpc.FailureFromError(err).GetCode() != want || !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("rejected selection mutated state or returned wrong failure")
				}
				return
			}
			if err != nil || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.ProfileId != profile.ProfileId || op.GetSelection().GetSelectedId() != "network-id" || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED {
				t.Fatal("current-network selection was not a completed no-op")
			}
			after := m.store.Read()
			before.RPCState, after.RPCState = nil, nil
			if !reflect.DeepEqual(clonePersistentConfig(before), clonePersistentConfig(after)) {
				t.Fatal("current selection altered registration or intent")
			}
			restarted, err := NewClientRPCMutations(m.store)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := restarted.selectNetworkAs(owner, request)
			if err != nil || !proto.Equal(op, replay) {
				t.Fatal("selection replay after coordinator restart changed outcome")
			}
		})
	}
}
