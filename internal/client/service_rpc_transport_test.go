//go:build windows || linux || darwin

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCLocalAcceptanceAndLostResponseRecovery(t *testing.T) {
	m := newRPCStoreTest(t)
	service := NewClientRPCService(m, &ipc.BuildIdentity{Version: "test"})
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
	server := local.NewServer(service.Handler())
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
	request.ControlOrigin = "https://control.example.test"
	if _, err := client.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetSupportInfo(ctx, connect.NewRequest(&ipc.GetSupportInfoRequest{}))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	events, err := client.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	if !events.Receive() || events.Msg().Sequence != 1 || events.Msg().GetSnapshot() == nil {
		t.Fatalf("missing first native snapshot: %v", events.Err())
	}
	accepted, err := client.CreateProfile(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if m.store.Read().LocalOwnerID == "" {
		t.Fatal("transport did not propagate OS identity into durable owner")
	}
	if !events.Receive() || events.Msg().Sequence != 2 || events.Msg().Metadata.Revision != accepted.Msg.Operation.Metadata.Revision {
		t.Fatalf("missing atomic refreshed snapshot: %v", events.Err())
	}
	if !events.Receive() || !proto.Equal(events.Msg().GetOperationChanged(), accepted.Msg.Operation) {
		t.Fatalf("missing native operation event: %v", events.Err())
	}
	if !events.Receive() || events.Msg().GetInvalidated().GetDomain() != ipc.Domain_DOMAIN_PROFILES {
		t.Fatalf("missing native invalidation: %v", events.Err())
	}
	_ = events.Close()
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
	if !proto.Equal(replayed.Msg.Operation, recovered.Msg.Operation) || len(m.store.Read().RPCState.Profiles) != 1 {
		t.Fatal("transport retry ran the mutation twice")
	}
	request.DisplayName = "different payload"
	_, err = client.CreateProfile(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	if len(m.store.Read().RPCState.Profiles) != 1 {
		t.Fatal("conflicting request performed side effects")
	}
}
