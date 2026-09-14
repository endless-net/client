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
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestResourcePublicTransportApplyReplayAndEvents(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-resource-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-resource-")
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
	client, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// CreateProfile claims ownership using the authenticated local transport.
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://control.example.test"
	created, err := client.CreateProfile(ctx, connect.NewRequest(create))
	if err != nil {
		t.Fatal(err)
	}
	profile := &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}
	opts, _ := signedServiceDNSFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = profile.ProfileId
		cfg.NodeID, cfg.NetworkID = opts.NetworkMap.Node.ID, opts.NetworkMap.Network.ID
		cfg.MapRevision, cfg.MapGlobalRevision = opts.NetworkMap.Network.Revision, opts.NetworkMap.Revision.Global
		cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
	request := &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id}
	_, err = client.SetResourceEnabled(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	workerCtx, stopWorker := context.WithCancel(ctx)
	done, err := s.StartProfileWorker(workerCtx, ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error {
		t.Error("disconnected resource operation started connection")
		return nil
	}, Stop: func(ctx context.Context) (ipc.ConnectionContinuity, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
		case <-ctx.Done():
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, ctx.Err()
		}
	}})
	if err != nil {
		stopWorker()
		t.Fatal(err)
	}
	defer func() { stopWorker(); <-done }()
	_, err = s.SetResourceEnabled(context.Background(), connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	events, err := client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	if !events.Receive() || events.Msg().GetSnapshot() == nil {
		t.Fatal("missing snapshot", events.Err())
	}
	request.Mutation = rpcCreateRequest(t, m).Mutation
	accepted, err := client.SetResourceEnabled(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_PENDING {
		t.Fatal("public admission skipped pending")
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("resource RPC did not wake worker")
	}
	if m.store.Read().ResourcePreferences != nil {
		t.Fatal("published before runtime effect")
	}
	// Both admission and terminal transition must invalidate the resource view.
	waitInvalidation := func(revision uint64) {
		t.Helper()
		for events.Receive() {
			event := events.Msg()
			if event.GetInvalidated().GetDomain() == ipc.Domain_DOMAIN_RESOURCES && event.Metadata.Revision == revision {
				if event.GetInvalidated().ProfileId != profile.ProfileId {
					t.Fatal("wrong invalidated profile")
				}
				return
			}
		}
		t.Fatal("resource invalidation missing", events.Err())
	}
	waitInvalidation(accepted.Msg.Operation.Metadata.Revision)
	close(release)
	var terminal *ipc.Operation
	for {
		result, err := client.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: request.Mutation.RequestId}}))
		if err != nil {
			t.Fatal(err)
		}
		if rpcOperationTerminal(result.Msg.Operation.State) {
			terminal = result.Msg.Operation
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("worker did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	if terminal.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal(terminal)
	}
	if enabled, exists := m.store.Read().ResourcePreferences[id]; !exists || enabled {
		t.Fatal("resource false not committed")
	}
	waitInvalidation(terminal.Metadata.Revision)
	replay, err := client.SetResourceEnabled(ctx, connect.NewRequest(request))
	if err != nil || !proto.Equal(replay.Msg.Operation, terminal) {
		t.Fatal("public replay lost committed operation", err)
	}
}
