//go:build windows || linux || darwin

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNetworkSelectionThroughNativeTransport(t *testing.T) {
	fixture, target := networkRegistrationExecutorFixture(t)
	source := networkSelectionContext(fixture.store.Read())
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-network-public-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-network-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := local.NewServer(s.Handler())
	serving := make(chan error, 1)
	go func() { serving <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-serving; err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://control.test"
	created, err := consumer.CreateProfile(ctx, connect.NewRequest(create))
	if err != nil {
		t.Fatal(err)
	}
	profile := &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}
	if err := m.store.Update(func(cfg *Config) error {
		state, owner := cfg.RPCState, cfg.LocalOwnerID
		*cfg = clonePersistentConfig(source)
		cfg.RPCState, cfg.LocalOwnerID = state, owner
		state.ActiveProfileID = profile.ProfileId
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
		target.LocalOwnerID = owner
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := &ipc.SelectNetworkRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, NetworkId: target.NetworkID}
	_, err = consumer.SelectNetwork(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var registrations, starts, stops atomic.Int32
	s.NetworksProvider = func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		return []*ipc.Network{{Id: source.NetworkID, AccountId: source.ActiveAccountID}, {Id: target.NetworkID, AccountId: source.ActiveAccountID}}, nil
	}
	s.NetworkSelectionProviders = ClientRPCNetworkSelectionProviders{Networks: s.NetworksProvider,
		Register: func(providerCtx context.Context, _ Config, input ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
			registrations.Add(1)
			if input.NetworkID != target.NetworkID {
				t.Error("unauthorized network reached registration")
			}
			if err := save(target); err != nil {
				return nil, err
			}
			close(entered)
			select {
			case <-release:
				return nil, nil
			case <-providerCtx.Done():
				return nil, providerCtx.Err()
			}
		}, Cleanup: func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error {
			t.Error("unexpected cleanup")
			return nil
		}}
	workerCtx, stopWorker := context.WithCancel(ctx)
	done, err := s.StartNetworkSelectionWorker(workerCtx, ClientRPCProfileDriver{Lock: &sync.Mutex{},
		Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			stops.Add(1)
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
		},
		Start: func(_ context.Context, cfg Config) error {
			starts.Add(1)
			if cfg.NodeID != target.NodeID || stops.Load() != 1 {
				t.Error("wrong target or missing teardown")
			}
			return nil
		}})
	if err != nil {
		stopWorker()
		t.Fatal(err)
	}
	var join sync.Once
	defer join.Do(func() { stopWorker(); <-done })
	info, err := consumer.GetRuntimeInfo(ctx, connect.NewRequest(&ipc.GetRuntimeInfoRequest{}))
	if err != nil || !networkCapabilityAvailable(info.Msg.Runtime) {
		t.Fatal("live worker capability missing", err)
	}
	catalog, err := consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	for _, network := range catalog.Msg.Networks {
		if network.Selection.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
			t.Fatal("ready catalog selection unavailable")
		}
	}
	_, err = s.SelectNetwork(context.Background(), connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	if err := m.store.Update(func(cfg *Config) error { cfg.PrivateKey = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = consumer.SelectNetwork(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
	if err := m.store.Update(func(cfg *Config) error { cfg.PrivateKey = source.PrivateKey; return nil }); err != nil {
		t.Fatal(err)
	}
	accepted, err := consumer.SelectNetwork(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("public selection did not wake registration")
	}
	replay, err := consumer.SelectNetwork(ctx, connect.NewRequest(request))
	if err != nil || replay.Msg.Operation.Id != accepted.Msg.Operation.Id || registrations.Load() != 1 {
		t.Fatal("pending replay registered twice", err)
	}
	conflict := proto.Clone(request).(*ipc.SelectNetworkRequest)
	conflict.NetworkId = source.NetworkID
	_, err = consumer.SelectNetwork(ctx, connect.NewRequest(conflict))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	busy := proto.Clone(request).(*ipc.SelectNetworkRequest)
	busy.Mutation = rpcCreateRequest(t, m).Mutation
	_, err = consumer.SelectNetwork(ctx, connect.NewRequest(busy))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	catalog, err = consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: profile}))
	if err != nil || catalog.Msg.Networks[0].Selection.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
		t.Fatal("pending selection advertised available", err)
	}
	once.Do(func() { close(release) })
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !rpcOperationTerminal(networkApplyOperation(t, m, accepted.Msg.Operation.Id).State) {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("selection did not finish")
		}
	}
	completed, err := consumer.SelectNetwork(ctx, connect.NewRequest(request))
	if err != nil || completed.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.Msg.Operation.GetSelection().SelectedId != target.NetworkID || starts.Load() != 1 || stops.Load() != 1 || registrations.Load() != 1 {
		t.Fatal("public selection did not complete/replay safely", err)
	}
	denied := &ipc.SelectNetworkRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, NetworkId: "not-authorized"}
	rejected, err := consumer.SelectNetwork(ctx, connect.NewRequest(denied))
	if err != nil {
		t.Fatal(err)
	}
	for !rpcOperationTerminal(networkApplyOperation(t, m, rejected.Msg.Operation.Id).State) {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("catalog rejection did not finish")
		}
	}
	if result := networkApplyOperation(t, m, rejected.Msg.Operation.Id); result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED || registrations.Load() != 1 || stops.Load() != 1 {
		t.Fatal("unauthorized catalog target caused effects", result)
	}
	join.Do(func() { stopWorker(); <-done })
	info, err = consumer.GetRuntimeInfo(ctx, connect.NewRequest(&ipc.GetRuntimeInfoRequest{}))
	if err != nil || networkCapabilityAvailable(info.Msg.Runtime) {
		t.Fatal("stopped worker retained capability", err)
	}
	replayedFailure, err := consumer.SelectNetwork(ctx, connect.NewRequest(denied))
	if err != nil || replayedFailure.Msg.Operation.Id != rejected.Msg.Operation.Id || replayedFailure.Msg.Operation.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED {
		t.Fatal("stopped worker prevented durable failure replay", err)
	}
	_, err = s.SelectNetwork(context.Background(), connect.NewRequest(denied))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	conflict = proto.Clone(denied).(*ipc.SelectNetworkRequest)
	conflict.NetworkId = "another-network"
	_, err = consumer.SelectNetwork(ctx, connect.NewRequest(conflict))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}

func networkCapabilityAvailable(info *ipc.RuntimeInfo) bool {
	for _, capability := range info.GetCapabilities() {
		if capability.Capability == ipc.Capability_CAPABILITY_NETWORK_SELECTION && capability.Restriction.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
			return true
		}
	}
	return false
}
