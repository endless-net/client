package clientipcconnect_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	clientipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
)

type service struct {
	clientipcconnect.UnimplementedClientServiceHandler
}

func (service) GetStatus(context.Context, *connect.Request[clientipc.GetStatusRequest]) (*connect.Response[clientipc.GetStatusResponse], error) {
	return connect.NewResponse(&clientipc.GetStatusResponse{
		Status: &clientipc.Status{ServiceState: clientipc.ServiceState_SERVICE_STATE_DISCONNECTED},
	}), nil
}

func (service) WatchEvents(_ context.Context, _ *connect.Request[clientipc.WatchEventsRequest], stream *connect.ServerStream[clientipc.WatchEventsResponse]) error {
	return stream.Send(&clientipc.WatchEventsResponse{
		Sequence: 1,
		Event: &clientipc.WatchEventsResponse_Snapshot{
			Snapshot: &clientipc.SnapshotEvent{
				Runtime: &clientipc.RuntimeInfo{Protocol: "endlessnet-client-ipc"},
			},
		},
	})
}

// Both wire protocols must support unary and server-streaming generated APIs.
// gRPC is the wire protocol used by the generated Dart client.
func TestGeneratedProtocols(t *testing.T) {
	_, handler := clientipcconnect.NewClientServiceHandler(service{})
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	for _, protocol := range []string{"connect", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			var options []connect.ClientOption
			if protocol == "grpc" {
				options = append(options, connect.WithGRPC())
			}
			client := clientipcconnect.NewClientServiceClient(server.Client(), server.URL, options...)
			status, err := client.GetStatus(t.Context(), connect.NewRequest(&clientipc.GetStatusRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			if status.Msg.GetStatus().GetServiceState() != clientipc.ServiceState_SERVICE_STATE_DISCONNECTED {
				t.Fatalf("unexpected status: %v", status.Msg)
			}
			stream, err := client.WatchEvents(t.Context(), connect.NewRequest(&clientipc.WatchEventsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if !stream.Receive() {
				t.Fatalf("missing snapshot: %v", stream.Err())
			}
			if stream.Msg().GetSequence() != 1 || stream.Msg().GetSnapshot().GetRuntime().GetProtocol() != "endlessnet-client-ipc" {
				t.Fatalf("unexpected event: %v", stream.Msg())
			}
			if stream.Receive() || stream.Err() != nil {
				t.Fatalf("unexpected stream termination: %v", stream.Err())
			}
		})
	}
}
