//go:build windows || linux || darwin

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"google.golang.org/protobuf/proto"
)

// This fixture exercises the real acceptance/store boundary. It deliberately
// does not claim to implement a profile provider or full production runtime.
type rpcAcceptanceFixture struct {
	clientipcconnect.UnimplementedClientServiceHandler
	mutations    *ClientRPCMutations
	preparations atomic.Int32
}

func (s *rpcAcceptanceFixture) CreateProfile(ctx context.Context, req *connect.Request[ipc.CreateProfileRequest]) (*connect.Response[ipc.CreateProfileResponse], error) {
	op, _, err := s.mutations.Accept(ctx, rpcCreateProfile, req.Msg, func(cfg *Config, op *ipc.Operation) error {
		s.preparations.Add(1)
		return rpcPrepareTest(cfg, op)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.CreateProfileResponse{Operation: op}), nil
}

func (s *rpcAcceptanceFixture) GetOperation(ctx context.Context, req *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
	op, err := s.mutations.GetOperation(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.GetOperationResponse{Operation: op}), nil
}

func TestRPCLocalAcceptanceAndLostResponseRecovery(t *testing.T) {
	m := newRPCStoreTest(t)
	fixture := &rpcAcceptanceFixture{mutations: m}
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-acceptance-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-rpc-")
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
	_, handler := clientipcconnect.NewClientServiceHandler(fixture,
		connect.WithInterceptors(rpc.Guard{Authorize: m.Authorize}),
		connect.WithReadMaxBytes(rpc.MaxRequestBytes), connect.WithSendMaxBytes(rpc.MaxResponseBytes))
	server := local.NewServer(handler)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
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
	request := rpcCreateRequest(t, m)
	accepted, err := client.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if m.store.Read().LocalOwnerID == "" {
		t.Fatal("transport did not propagate OS identity into durable owner")
	}
	lookup := &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: request.Mutation.RequestId}}
	// A newly attached consumer has only its original request ID, not an op ID.
	client.Close()
	recovered, err := client.GetOperation(ctx, connect.NewRequest(lookup))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(accepted.Msg.Operation, recovered.Msg.Operation) || recovered.Msg.Operation.Kind != ipc.OperationKind_OPERATION_KIND_CREATE_PROFILE {
		t.Fatal("reconnection did not recover the identified original operation")
	}
	replayed, err := client.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replayed.Msg.Operation, recovered.Msg.Operation) || fixture.preparations.Load() != 1 {
		t.Fatal("transport retry ran the mutation twice")
	}
	request.DisplayName = "different payload"
	_, err = client.CreateProfile(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	if fixture.preparations.Load() != 1 {
		t.Fatal("conflicting request performed side effects")
	}
}
