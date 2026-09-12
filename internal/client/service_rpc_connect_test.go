package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func rpcConnectFixture(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.ProfileRef) {
	t.Helper()
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://control.test"
	p, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = p.ProfileId
		cfg.NodeID = "registered-node"
		// Domain tests substitute the driver; real driver verification has its
		// own tests and must reject this unsigned synthetic map.
		cfg.CachedMap = &clientapi.RegisterNodeResponse{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, peer, &ipc.ProfileRef{ProfileId: p.ProfileId}
}

func TestRPCConnectDurabilityAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m, peer, profile := rpcConnectFixture(t)
		r := &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
		op, err := m.connectAs(peer, r)
		if err != nil {
			t.Fatal(err)
		}
		if op.State != ipc.OperationState_OPERATION_STATE_PENDING || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
			t.Fatal("acceptance did not persist connected intent")
		}
		retry, err := m.connectAs(peer, r)
		if err != nil || retry.Id != op.Id {
			t.Fatal("connect retry was not idempotent", err)
		}
		starts, stops := 0, 0
		driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
			starts++
			if fail {
				return errors.New("private apply failure")
			}
			return nil
		}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			stops++
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
		}}
		if err := m.ReconcileConnect(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if starts != 1 || m.store.Read().NodeID != "registered-node" || m.store.Read().RPCState.ConnectOperationID != "" {
			t.Fatal("connect execution did not retain registration/finish plan")
		}
		if fail {
			if stops != 1 || final.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("failed apply not cleaned up")
			}
		} else if stops != 0 || final.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			t.Fatal("successful connect outcome")
		}
		if err := m.ReconcileConnect(t.Context(), driver); err != nil || starts != 1 {
			t.Fatal("completed connect repeated side effects", err)
		}
	}
}

func TestRPCDisconnectSupersedesConnect(t *testing.T) {
	for _, duringApply := range []bool{false, true} {
		m, peer, profile := rpcConnectFixture(t)
		op, err := m.connectAs(peer, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		disconnect := func() {
			if _, err := m.disconnectAs(peer, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
				t.Fatal(err)
			}
		}
		if !duringApply {
			disconnect()
		}
		starts, stops := 0, 0
		driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { starts++; disconnect(); return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			stops++
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
		}}
		if err := m.ReconcileConnect(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		if err := m.ReconcileDisconnect(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil || final.State != ipc.OperationState_OPERATION_STATE_CANCELLED || final.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_CANCELLED || stops != 1 || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
			t.Fatal("connect defeated explicit disconnect", err)
		}
		if (duringApply && starts != 1) || (!duringApply && starts != 0) {
			t.Fatal("superseded connect unexpectedly applied")
		}
	}
}

func TestRPCConnectResumesAfterLifecycleCancellation(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	op, err := m.connectAs(peer, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { cancel(); return context.Canceled }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Error("shutdown treated as business failure")
		return 0, nil
	}}
	if err := m.ReconcileConnect(ctx, driver); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	store, err := OpenConfigStore(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	driver.Start = func(context.Context, Config) error { return nil }
	if err := restarted.ReconcileConnect(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	final, err := restarted.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || final.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("connect restart failed", err)
	}
}
