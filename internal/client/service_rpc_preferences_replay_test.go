package client

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestPreferenceResourceReplayWithoutWorker(t *testing.T) {
	for _, method := range []string{"SetPreferences", "ResetPreferences", "SetResourceEnabled"} {
		t.Run(method, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			mutation := rpcCreateRequest(t, m).Mutation
			var request proto.Message
			var accepted *ipc.Operation
			var err error
			switch method {
			case "SetPreferences":
				r := &ipc.SetPreferencesRequest{Mutation: mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}}
				request = r
				accepted, err = m.setNetworkPreferencesAs(owner, r)
			case "ResetPreferences":
				r := &ipc.ResetPreferencesRequest{Mutation: mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS}}
				request = r
				accepted, err = m.resetNetworkPreferencesAs(owner, r)
			case "SetResourceEnabled":
				id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
				r := &ipc.SetResourceEnabledRequest{Mutation: mutation, Profile: profile, ResourceId: id, Enabled: false}
				request = r
				accepted, err = m.setResourceEnabledAs(owner, r)
			}
			if err != nil {
				t.Fatal(err)
			}
			procedure := "/client.v0.ClientService/" + method
			for _, terminal := range []bool{false, true} {
				if terminal {
					if err := m.ReconcileNetworkPreferences(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
						return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
					}}); err != nil {
						t.Fatal(err)
					}
					accepted, err = m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: accepted.Id}})
					if err != nil || accepted.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
						t.Fatal("fixture did not complete")
					}
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				s := NewClientRPCService(m, nil)
				for _, stopped := range []bool{false, true} {
					if stopped {
						ctx, cancel := context.WithCancel(t.Context())
						cancel()
						s.profileWorker = &clientRPCProfileWorker{ctx: ctx}
					}
					before := clonePersistentConfig(m.store.Read())
					call := func(peer local.Peer, payload proto.Message) (*ipc.Operation, error) {
						return s.acceptNetworkPreferenceOperation(peer, procedure, payload, func() (*ipc.Operation, error) { t.Fatal("unavailable worker admitted effects"); return nil, nil })
					}
					replayed, err := call(owner, request)
					if err != nil || !proto.Equal(replayed, accepted) {
						t.Fatal("worker state hid durable replay", err)
					}
					_, err = call(local.Peer{}, request)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
					_, err = call(local.Peer{Identity: "observer"}, request)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
					conflict := proto.Clone(request)
					switch v := conflict.(type) {
					case *ipc.SetPreferencesRequest:
						v.Patch.AcceptDns = proto.Bool(true)
					case *ipc.ResetPreferencesRequest:
						v.Keys = []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES}
					case *ipc.SetResourceEnabledRequest:
						v.Enabled = true
					}
					_, err = call(owner, conflict)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
					fresh := proto.Clone(request)
					freshMutation := rpcCreateRequest(t, m).Mutation
					freshMutation.RequestId = "6b160000-0000-4000-8000-000000000001"
					switch v := fresh.(type) {
					case *ipc.SetPreferencesRequest:
						v.Mutation = freshMutation
					case *ipc.ResetPreferencesRequest:
						v.Mutation = freshMutation
					case *ipc.SetResourceEnabledRequest:
						v.Mutation = freshMutation
					}
					_, err = call(owner, fresh)
					assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
					if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
						t.Fatal("replay or rejection mutated durable state")
					}
				}
			}
		})
	}
}
