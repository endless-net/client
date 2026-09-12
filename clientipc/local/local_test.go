//go:build windows || linux || darwin

package local

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
	"github.com/endless-net/client/clientipc/rpc"
	clientipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
)

type testService struct {
	clientipcconnect.UnimplementedClientServiceHandler
	digest string
}

func (s testService) GetRuntimeInfo(context.Context, *connect.Request[clientipc.GetRuntimeInfoRequest]) (*connect.Response[clientipc.GetRuntimeInfoResponse], error) {
	return connect.NewResponse(&clientipc.GetRuntimeInfoResponse{Runtime: &clientipc.RuntimeInfo{
		Protocol: rpc.Protocol, IpcVersion: rpc.Version, ContractSha256: s.digest, InstanceId: "test-instance",
	}}), nil
}

func (testService) GetStatus(context.Context, *connect.Request[clientipc.GetStatusRequest]) (*connect.Response[clientipc.GetStatusResponse], error) {
	return connect.NewResponse(&clientipc.GetStatusResponse{Status: &clientipc.Status{
		ServiceState: clientipc.ServiceState_SERVICE_STATE_DISCONNECTED,
	}}), nil
}

func (testService) WatchEvents(_ context.Context, _ *connect.Request[clientipc.WatchEventsRequest], stream *connect.ServerStream[clientipc.WatchEventsResponse]) error {
	return stream.Send(&clientipc.WatchEventsResponse{Sequence: 1, Event: &clientipc.WatchEventsResponse_Snapshot{
		Snapshot: &clientipc.SnapshotEvent{Runtime: &clientipc.RuntimeInfo{InstanceId: "test-instance"}},
	}})
}

func TestLocalGRPC(t *testing.T) {
	for _, digest := range []string{rpc.Digest(), "mismatched-installation"} {
		t.Run(digest, func(t *testing.T) {
			endpoint := filepath.Join(t.TempDir(), "rpc.sock")
			if runtime.GOOS == "windows" {
				endpoint = fmt.Sprintf(`\\.\pipe\endlessnet-rpc-test-%d`, time.Now().UnixNano())
			} else {
				// Darwin sockaddr_un has a short path limit; t.TempDir includes
				// the full test name and can exceed it before the socket suffix.
				dir, err := os.MkdirTemp("/tmp", "en-ipc-")
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
			listener, err := Listen(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			_, handler := clientipcconnect.NewClientServiceHandler(testService{digest: digest}, connect.WithInterceptors(rpc.Guard{
				Authorize: func(ctx context.Context, _ clientipc.Access, _ string) error {
					peer, ok := PeerFromContext(ctx)
					if !ok || peer.Identity == "" {
						return rpc.Error(connect.CodeUnauthenticated, clientipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
					}
					return nil
				},
			}))
			server := NewServer(handler)
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			defer func() {
				_ = server.Close()
				if err := <-done; err != http.ErrServerClosed {
					t.Errorf("serve: %v", err)
				}
			}()
			client, err := NewClient(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = client.Bootstrap(ctx)
			if digest != rpc.Digest() {
				if rpc.FailureFromError(err).GetCode() != clientipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH {
					t.Fatalf("bootstrap accepted bad pairing: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status, err := client.GetStatus(ctx, connect.NewRequest(&clientipc.GetStatusRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			if status.Msg.GetStatus().GetServiceState() != clientipc.ServiceState_SERVICE_STATE_DISCONNECTED {
				t.Fatal(status.Msg)
			}
			stream, err := client.WatchEvents(ctx, connect.NewRequest(&clientipc.WatchEventsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()
			if !stream.Receive() || stream.Msg().GetSnapshot().GetRuntime().GetInstanceId() != "test-instance" {
				t.Fatalf("missing snapshot: %v", stream.Err())
			}
			if stream.Receive() || stream.Err() != nil {
				t.Fatalf("stream did not finish cleanly: %v", stream.Err())
			}
		})
	}
}

func TestRejectNonlocalEndpointAndMissingPeer(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:8765", "relative.sock", `\\remote\pipe\endlessnet-service`} {
		if _, err := NewClient(endpoint); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
	if _, ok := PeerFromContext(t.Context()); ok {
		t.Fatal("unauthenticated context has a peer")
	}
}
