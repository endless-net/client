package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCProfileWorkerShutdownAndStartupRecovery(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://control.test"
	created, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	op, err := m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
	if err != nil {
		t.Fatal(err)
	}
	service := NewClientRPCService(m, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(ctx context.Context) (ipc.ConnectionContinuity, error) {
		close(entered)
		<-ctx.Done()
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, ctx.Err()
	}, Start: func(context.Context, Config) error { t.Error("cancelled switch started target"); return nil }}
	done, err := service.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not recover persisted intent")
	}
	_, err = service.StartProfileWorker(ctx, driver)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	pending, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || pending.State != ipc.OperationState_OPERATION_STATE_RUNNING || m.store.Read().RPCState.ProfileSwitch == nil {
		t.Fatal("shutdown terminalized durable intent", err)
	}
	// Resume without issuing another SelectProfile request.
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()
	driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}
	done2, err := service.StartProfileWorker(ctx2, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel2(); <-done2 }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("resumed operation did not finish")
		case <-tick.C:
			final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			if final.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				return
			}
		}
	}
}
