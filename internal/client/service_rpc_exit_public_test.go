package client

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestExitReplaySurvivesWorkerShutdownAndRestart(t *testing.T) {
	for _, method := range []string{"SelectExitNode", "ClearExitNode"} {
		t.Run(method, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			modes := []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &networkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			mutation := rpcCreateRequest(t, m).Mutation
			var request proto.Message
			var accepted *ipc.Operation
			var err error
			if method == "SelectExitNode" {
				r := &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}
				request = r
				accepted, err = m.selectExitNodeAs(owner, r, modes)
			} else {
				r := &ipc.ClearExitNodeRequest{Mutation: mutation, Profile: profile}
				request = r
				accepted, err = m.clearExitNodeAs(owner, r)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, completed := range []bool{false, true} {
				if completed {
					executor := clientRPCExitExecutor{Lock: &sync.Mutex{}, Modes: modes, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
						return appliedExitTestStatus(profile.ProfileId, method == "SelectExitNode"), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
					}}
					if err := m.reconcileExitChange(t.Context(), executor); err != nil {
						t.Fatal(err)
					}
					accepted, err = m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: accepted.Id}})
					if err != nil || accepted.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
						t.Fatal("fixture failed to complete", err)
					}
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				service := NewClientRPCService(m, nil)
				for _, cancelled := range []bool{false, true} {
					if cancelled {
						ctx, cancel := context.WithCancel(t.Context())
						cancel()
						service.exitWorker = &clientRPCProfileWorker{ctx: ctx}
					}
					call := func(peer local.Peer, payload proto.Message) (*ipc.Operation, error) {
						return service.acceptExitOperation(peer, "/client.v0.ClientService/"+method, payload, func([]clientRPCExitMode) (*ipc.Operation, error) {
							t.Fatal("unavailable worker admitted fresh work")
							return nil, nil
						})
					}
					before := clonePersistentConfig(m.store.Read())
					replayed, err := call(owner, request)
					if err != nil || !proto.Equal(replayed, accepted) {
						t.Fatal("worker readiness hid durable replay", err)
					}
					_, err = call(local.Peer{}, request)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
					_, err = call(local.Peer{Identity: "observer"}, request)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
					conflict := proto.Clone(request)
					fresh := proto.Clone(request)
					freshMutation := rpcCreateRequest(t, m).Mutation
					freshMutation.RequestId = "6b170000-0000-4000-8000-000000000001"
					switch r := conflict.(type) {
					case *ipc.SelectExitNodeRequest:
						r.LanAccess = ipc.LanAccess_LAN_ACCESS_ALLOW
						fresh.(*ipc.SelectExitNodeRequest).Mutation = freshMutation
					case *ipc.ClearExitNodeRequest:
						r.Profile.ProfileId = "different-profile"
						fresh.(*ipc.ClearExitNodeRequest).Mutation = freshMutation
					}
					_, err = call(owner, conflict)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
					_, err = call(owner, fresh)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
					if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
						t.Fatal("replay or rejection changed persistent state")
					}
				}
			}
		})
	}
}

func TestExitPublicMethodsAuthenticateWithoutWorker(t *testing.T) {
	m, _, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	_, err := s.ClearExitNode(t.Context(), connect.NewRequest(&ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	_, err = s.SelectExitNode(t.Context(), connect.NewRequest(&ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
}

func TestExitAcceptanceQueuesWakeAfterDurableAdmission(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	w := &clientRPCProfileWorker{ctx: t.Context(), wake: make(chan struct{}, 1)}
	s.exitWorker = w
	s.exitModes = []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}}
	r := &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
	op, err := s.acceptExitOperation(owner, "/client.v0.ClientService/ClearExitNode", r, func(modes []clientRPCExitMode) (*ipc.Operation, error) {
		if !reflect.DeepEqual(modes, s.exitModes) {
			t.Fatal("admission lost executor mode constraints")
		}
		return m.clearExitNodeAs(owner, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.wake:
		stored := reopenRPCStoreFromDisk(t, m.store).Read()
		if stored.RPCState.ExitChange == nil || stored.RPCState.ExitChange.OperationID != op.Id {
			t.Fatal("wake preceded durable acceptance")
		}
	default:
		t.Fatal("accepted operation did not wake worker")
	}
}
