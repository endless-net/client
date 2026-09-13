package client

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCAdministratorBundleScopeAndRestart(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "unowned_installation", true: "another_owner"}[owned], func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if !owned {
				if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = ""; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			admin := local.Peer{Identity: "separate-administrator", Administrator: true}
			request := &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			op, err := m.createBundleAs(admin, request)
			if err != nil {
				t.Fatal(err)
			}
			// A durable plan must retain its admitted principal and installation
			// scope even when the caller is not the installation owner.
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bundles.state")
			s := NewClientRPCService(m, nil)
			s.bundleStore, err = openClientRPCBundleStore(path, m.now)
			if err != nil {
				t.Fatal(err)
			}
			s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
				return ClientRPCDiagnosticsObservation{}, nil
			}
			if err := s.reconcileBundles(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(admin, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatalf("administrator collection failed: state=%d code=%d lookup_error=%t", result.GetState(), result.GetFailure().GetCode(), err != nil)
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			s = NewClientRPCService(m, nil)
			s.bundleStore, err = openClientRPCBundleStore(path, m.now)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.purgeBundles(); err != nil {
				t.Fatal(err)
			}
			read := &ipc.ReadDiagnosticsBundleRequest{BundleId: op.Id}
			chunk, err := s.readDiagnosticsBundleAs(t.Context(), admin, read)
			if err != nil || len(chunk.GetData()) == 0 || !chunk.GetEof() {
				t.Fatal("administrator archive lost after restart")
			}
			for _, other := range []local.Peer{owner, {Identity: "another-administrator", Administrator: true}, {Identity: admin.Identity}} {
				if _, err := s.readDiagnosticsBundleAs(t.Context(), other, read); err == nil {
					t.Fatal("archive was exposed outside its admitted principal")
				}
			}
			if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "replacement-owner"; return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := s.readDiagnosticsBundleAs(t.Context(), admin, read); err == nil {
				t.Fatal("administrator archive survived installation ownership change")
			}
			if err := s.purgeBundles(); err != nil || len(s.bundleStore.items) != 0 {
				t.Fatal("revoked administrator archive was not purged")
			}
		})
	}
}
