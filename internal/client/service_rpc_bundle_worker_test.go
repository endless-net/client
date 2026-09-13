package client

import (
	"context"
	"errors"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCBundleWorkerShutdownAndStartupRecovery(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	op, err := m.createBundleAs(peer, &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	s.bundleStore, err = openClientRPCBundleStore(m.store.path+".bundles", m.now)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	s.DiagnosticsProvider = func(ctx context.Context) (ClientRPCDiagnosticsObservation, error) {
		close(started)
		<-ctx.Done()
		return ClientRPCDiagnosticsObservation{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done, err := s.startBundleWorker(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not scan pending plan")
	}
	_, err = s.startBundleWorker(ctx)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("shutdown lost cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not drain")
	}
	if s.bundleWorker != nil {
		t.Fatal("stopped worker still registered")
	}
	result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_RUNNING || len(m.store.Read().RPCState.Bundles) != 1 {
		t.Fatal("shutdown terminalized or lost unfinished plan", err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	s = NewClientRPCService(m, nil)
	s.bundleStore, err = openClientRPCBundleStore(m.store.path+".bundles", m.now)
	if err != nil {
		t.Fatal(err)
	}
	s.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
		return ClientRPCDiagnosticsObservation{OSVersion: "restarted"}, nil
	}
	ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
	done, err = s.startBundleWorker(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("restarted worker did not stop")
		}
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		result, err = m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if rpcOperationTerminal(result.State) {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("startup did not recover running operation")
		}
	}
	if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || result.GetBundle().GetBundleId() != op.Id || result.Metadata.InstanceId != m.instanceID {
		t.Fatal("invalid recovered operation", result)
	}
	if len(m.store.Read().RPCState.Bundles) != 0 {
		t.Fatal("completed plan retained")
	}
}
