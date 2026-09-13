package client

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCBundleCreateAndRestartRecovery(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "collect", true: "restore_saved_artifact"}[saved], func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			request := &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			op, err := m.createBundleAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			if op.State != ipc.OperationState_OPERATION_STATE_PENDING || op.Kind != ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE || op.ProfileId != profile.ProfileId {
				t.Fatal("incorrect admission")
			}
			path := filepath.Join(t.TempDir(), "bundles.state")
			store, err := openClientRPCBundleStore(path, m.now)
			if err != nil {
				t.Fatal(err)
			}
			var savedMetadata *ipc.BundleResult
			if saved {
				if _, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
					op.State = ipc.OperationState_OPERATION_STATE_RUNNING
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				savedMetadata, err = store.putID(op.Id, peer.Identity, peer.Identity, profile.ProfileId, []byte("saved archive"))
				if err != nil {
					t.Fatal(err)
				}
			}
			// Recreate coordinator and artifact storage: no request wakeup is needed.
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			s.bundleStore, err = openClientRPCBundleStore(path, m.now)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				calls++
				return ClientRPCDiagnosticsObservation{OSVersion: "test"}, nil
			}
			if err := s.reconcileBundles(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || result.GetBundle().GetBundleId() != op.Id {
				t.Fatal("bundle did not complete", result, err)
			}
			if saved && (calls != 0 || !proto.Equal(savedMetadata, result.GetBundle())) {
				t.Fatal("restart recollected or changed artifact")
			}
			if !saved && calls != 1 {
				t.Fatal("missing collection")
			}
			chunk, err := s.readDiagnosticsBundleAs(t.Context(), peer, &ipc.ReadDiagnosticsBundleRequest{BundleId: op.Id})
			if err != nil || len(chunk.GetData()) == 0 || !chunk.Eof {
				t.Fatal("published archive unavailable", err)
			}
			// A second restart must restore the completed journal and artifact,
			// not merely the previously running operation.
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			s = NewClientRPCService(m, nil)
			s.bundleStore, err = openClientRPCBundleStore(path, m.now)
			if err != nil {
				t.Fatal(err)
			}
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				t.Error("completed bundle was collected again after restart")
				return ClientRPCDiagnosticsObservation{}, errors.New("unexpected collection")
			}
			if err := s.reconcileBundles(t.Context()); err != nil {
				t.Fatal(err)
			}
			restored, err := s.readDiagnosticsBundleAs(t.Context(), peer, &ipc.ReadDiagnosticsBundleRequest{BundleId: op.Id})
			if err != nil || !restored.GetEof() || !bytes.Equal(chunk.Data, restored.GetData()) {
				t.Fatal("completed bundle bytes changed after restart")
			}
			replay, err := m.createBundleAs(peer, request)
			if err != nil || !proto.Equal(replay, result) {
				t.Fatal("retry changed result", err)
			}
			if len(m.store.Read().RPCState.Bundles) != 0 {
				t.Fatal("terminal plan retained")
			}
		})
	}
}

func TestRPCBundleExecutionFailures(t *testing.T) {
	for _, mode := range []string{"owner", "profile", "provider", "cancel", "persist", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			op, err := m.createBundleAs(peer, &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			s.bundleStore = &clientRPCBundleStore{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "persist" {
				s.bundleStore.persist = func(map[string]clientRPCBundleRecord) error { return errors.New("synthetic write failure") }
			}
			if mode == "capacity" {
				for range 32 {
					if _, err := s.bundleStore.put(peer.Identity, profile.ProfileId, []byte("archive")); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				switch mode {
				case "cancel":
					cancel()
				case "provider":
					return ClientRPCDiagnosticsObservation{}, errors.New("private provider failure")
				case "owner", "profile":
					if err := m.store.Update(func(cfg *Config) error {
						if mode == "owner" {
							cfg.LocalOwnerID = "new-owner"
						} else {
							cfg.RPCState.ActiveProfileID = "other"
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				return ClientRPCDiagnosticsObservation{}, nil
			}
			err = s.reconcileBundles(ctx)
			if mode == "cancel" || mode == "persist" {
				if err == nil || len(m.store.Read().RPCState.Bundles) != 1 {
					t.Fatal("interruption lost recoverable plan", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			for _, record := range cfg.RPCState.Operations {
				result := &ipc.Operation{}
				if err := proto.Unmarshal(record.Operation, result); err != nil {
					t.Fatal(err)
				}
				if result.Id == op.Id && (result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure() == nil || result.GetBundle() != nil) {
					t.Fatal("failed collection reported success", result)
				}
			}
			if len(cfg.RPCState.Bundles) != 0 {
				t.Fatal("failed plan retained")
			}
		})
	}
}
