package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNetworkWorkerResumesAllPhasesFromDisk(t *testing.T) {
	m, target := networkRegistrationExecutorFixture(t)
	id := m.store.Read().RPCState.NetworkSelection.OperationID
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.NetworkSelection.Target = nil
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
		cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	var catalogs, registrations, starts, stops atomic.Int32
	s.NetworkSelectionProviders = ClientRPCNetworkSelectionProviders{
		Networks: func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
			catalogs.Add(1)
			return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
		},
		Register: func(_ context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
			registrations.Add(1)
			return nil, save(target)
		},
		Cleanup: func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error {
			t.Error("successful worker invoked compensation")
			return nil
		},
	}
	s.NetworksProvider = s.NetworkSelectionProviders.Networks
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(_ context.Context, cfg Config) error {
		starts.Add(1)
		if cfg.NodeID != target.NodeID || cfg.NetworkID != target.NetworkID || stops.Load() != 1 {
			t.Error("worker applied wrong target or skipped source teardown")
		}
		return nil
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops.Add(1)
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done, err := s.StartNetworkSelectionWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartNetworkSelectionWorker(ctx, driver); err == nil {
		t.Fatal("duplicate worker accepted")
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !rpcOperationTerminal(networkApplyOperation(t, m, id).State) {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("worker did not advance durable phases")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not join")
	}
	if op := networkApplyOperation(t, m, id); op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || catalogs.Load() != 1 || registrations.Load() != 1 || starts.Load() != 1 || stops.Load() != 1 {
		t.Fatal("worker repeated or skipped a phase", op)
	}
}

func TestNetworkHostJoinsRegistrationOnShutdown(t *testing.T) {
	m, _ := networkRegistrationExecutorFixture(t)
	s := NewClientRPCService(m, nil)
	entered, exited := make(chan struct{}), make(chan struct{})
	s.NetworkSelectionProviders = ClientRPCNetworkSelectionProviders{
		Networks: func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) { return nil, nil },
		Register: func(ctx context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, _ func(Config) error) (*ipc.UserAction, error) {
			close(entered)
			<-ctx.Done()
			close(exited)
			return nil, ctx.Err()
		},
		Cleanup: func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error { return nil },
	}
	s.NetworksProvider = s.NetworkSelectionProviders.Networks
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener := &rpcHostBlockingListener{closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, listener, driver, func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error) {
			return nil, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host did not resume network registration")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not join network worker")
	}
	select {
	case <-exited:
	default:
		t.Fatal("host returned before provider exit")
	}
	if s.networkWorker != nil || m.store.Read().RPCState.NetworkSelection == nil {
		t.Fatal("host leaked worker or discarded unfinished registration")
	}
}

func TestNetworkRemoteCompensationDoesNotBlockDisconnect(t *testing.T) {
	m, _ := readyNetworkActivationFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var stops atomic.Int32
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops.Add(1)
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	aborted := make(chan error, 1)
	go func() {
		aborted <- m.ReconcileNetworkSelectionAbort(t.Context(), driver, func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error {
			close(entered)
			<-release
			return nil
		}, ipc.ErrorCode_ERROR_CODE_CANCELLED)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not start")
	}
	cfg := m.store.Read()
	op, err := m.disconnectAs(local.Peer{Identity: cfg.LocalOwnerID}, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: cfg.RPCState.ActiveProfileID}})
	if err != nil {
		t.Fatal(err)
	}
	disconnected := make(chan error, 1)
	go func() { disconnected <- m.ReconcileDisconnect(t.Context(), driver) }()
	select {
	case err := <-disconnected:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote compensation blocked local Disconnect")
	}
	if stops.Load() != 1 || networkApplyOperation(t, m, op.Id).State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("Disconnect did not confirm local teardown")
	}
	once.Do(func() { close(release) })
	select {
	case err := <-aborted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("compensation did not finish")
	}
}
