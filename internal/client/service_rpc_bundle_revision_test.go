package client

import (
	"context"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCBundleRejectsConcurrentRevisionChange(t *testing.T) {
	for _, phase := range []string{"collection", "publication"} {
		t.Run(phase, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			request := &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			op, err := m.createBundleAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			s.bundleStore = &clientRPCBundleStore{}
			changeRevision := func() error {
				return m.store.Update(func(cfg *Config) error {
					cfg.RPCState.Revision++
					return nil
				})
			}
			calls := 0
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				calls++
				if phase == "collection" {
					if err := changeRevision(); err != nil {
						t.Fatal(err)
					}
				}
				return ClientRPCDiagnosticsObservation{}, nil
			}
			if phase == "publication" {
				s.bundleStore.persist = func(map[string]clientRPCBundleRecord) error { return changeRevision() }
			}
			if err := s.reconcileBundles(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.GetState() != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE || result.GetBundle() != nil {
				t.Fatal("concurrent revision change did not reject the bundle")
			}
			if phase == "publication" && result.GetFailure().GetReasonKey() != "bundle_snapshot_changed" {
				t.Fatal("publication snapshot race was misclassified as a scope change")
			}
			if len(m.store.Read().RPCState.Bundles) != 0 {
				t.Fatal("terminal failure retained a collection plan")
			}
			chunk, err := s.readDiagnosticsBundleAs(t.Context(), peer, &ipc.ReadDiagnosticsBundleRequest{BundleId: op.Id})
			if err == nil || chunk != nil {
				t.Fatal("failed operation exposed unpublished archive bytes")
			}
			replay, err := m.createBundleAs(peer, request)
			if err != nil || !proto.Equal(replay, result) {
				t.Fatal("exact request replay changed the stale result")
			}
			if err := s.reconcileBundles(t.Context()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("terminal stale operation was collected again")
			}
		})
	}
}
