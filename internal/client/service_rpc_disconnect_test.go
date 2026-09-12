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

func TestRPCDisconnectReservedCapacityAndRegistration(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://control.test"
	profile, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = profile.ProfileId
		cfg.NodeID = "retained-node"
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rpcMaxNonterminalOperations-1; i++ {
		if _, _, err := m.acceptAs(peer, rpcCreateProfile, rpcCreateRequest(t, m), rpcPrepareTest); err != nil {
			t.Fatal(err)
		}
	}
	request := &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profile.ProfileId}}
	op, err := m.disconnectAs(peer, request)
	if err != nil {
		t.Fatal("Disconnect rejected reserved slot", err)
	}
	if op.State != ipc.OperationState_OPERATION_STATE_PENDING || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("disconnect intent was not durable before effects")
	}
	retry, err := m.disconnectAs(peer, request)
	if err != nil || retry.Id != op.Id {
		t.Fatal("disconnect retry failed at full capacity", err)
	}
	stops := 0
	if err := m.ReconcileDisconnect(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}); err != nil {
		t.Fatal(err)
	}
	final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || final.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || stops != 1 || m.store.Read().NodeID != "retained-node" {
		t.Fatal("disconnect completion lost registration", err)
	}
	snapshot, err := m.snapshotAs(peer, nil)
	if err != nil || snapshot.Status.GetIntent().DesiredState != ipc.DesiredState_DESIRED_STATE_DISCONNECTED || snapshot.Status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
		t.Fatal("disconnect snapshot has stale intent/phase", err)
	}
}

func TestRPCDisconnectDuringProfileApply(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://target.test"
	profile, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		p := cfg.RPCState.Profiles[profile.ProfileId]
		p.Configuration.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.RPCState.Profiles[profile.ProfileId] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profile.ProfileId}}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}, Start: func(ctx context.Context, _ Config) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	done := make(chan error, 1)
	go func() { done <- m.ReconcileProfileSwitch(ctx, driver) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("profile apply not reached")
	}
	op, err := m.disconnectAs(peer, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profile.ProfileId}})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileDisconnect(ctx, driver); err != nil {
		t.Fatal(err)
	}
	final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || final.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("switch restored connected intent after explicit disconnect", err)
	}
}

func TestRPCDisconnectFailureAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		m := newRPCStoreTest(t)
		peer := local.Peer{Identity: "uid:1000"}
		r := rpcCreateRequest(t, m)
		r.ControlOrigin = "https://control.test"
		p, err := m.createProfileAs(peer, r)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ActiveProfileID = p.ProfileId; return nil }); err != nil {
			t.Fatal(err)
		}
		op, err := m.disconnectAs(peer, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: p.ProfileId}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			if cancelled {
				cancel()
			}
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("private driver error")
		}}
		err = m.ReconcileDisconnect(ctx, driver)
		cancel()
		if cancelled && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if !cancelled && err != nil {
			t.Fatal(err)
		}
		final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if cancelled {
			if final.State != ipc.OperationState_OPERATION_STATE_RUNNING || m.store.Read().RPCState.DisconnectOperationID == "" {
				t.Fatal("shutdown lost pending disconnect")
			}
			store, err := OpenConfigStore(m.store.path)
			if err != nil {
				t.Fatal(err)
			}
			restarted, err := NewClientRPCMutations(store)
			if err != nil {
				t.Fatal(err)
			}
			driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}
			if err := restarted.ReconcileDisconnect(t.Context(), driver); err != nil {
				t.Fatal(err)
			}
			recovered, err := restarted.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || recovered.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || restarted.store.Read().RPCState.DisconnectOperationID != "" {
				t.Fatal("restart did not finish durable disconnect", err)
			}
		} else if final.State != ipc.OperationState_OPERATION_STATE_FAILED || final.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || final.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN {
			t.Fatal("failed Down claimed success")
		}
	}
}
