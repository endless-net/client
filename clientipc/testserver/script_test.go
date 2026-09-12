package testserver

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	pb "github.com/endless-net/client/clientipc/v0"
)

func TestLoadScriptAndTypedFailure(t *testing.T) {
	server, err := Load(strings.NewReader(`{"steps":[
	 {"method":"GetStatus","request":{},"responses":[{"status":{"connectionPhase":"CONNECTION_PHASE_CONNECTING"}}]},
	 {"method":"GetStatus","request":{},"failure":{"rpc_code":"unavailable","detail":{"code":"ERROR_CODE_UNAVAILABLE","retryable":true}}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	client := start(t, server)
	response, err := client.GetStatus(t.Context(), connect.NewRequest(&pb.GetStatusRequest{}))
	if err != nil || response.Msg.GetStatus().GetConnectionPhase() != pb.ConnectionPhase_CONNECTION_PHASE_CONNECTING {
		t.Fatal("script response lost")
	}
	_, err = client.GetStatus(t.Context(), connect.NewRequest(&pb.GetStatusRequest{}))
	if connect.CodeOf(err) != connect.CodeUnavailable || !rpc.FailureFromError(err).GetRetryable() {
		t.Fatal("typed failure lost")
	}
	if err := server.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidScriptDoesNotExposeInput(t *testing.T) {
	for _, raw := range []string{
		`{"steps":[]}`, `{"steps":[],"secret":"synthetic-sensitive-marker"}`, `{"steps":[]} {}`,
		`{"steps":[{"method":"Unknown","request":{}}]}`,
		`{"steps":[{"method":"GetStatus","request":{"synthetic-sensitive-marker":true},"responses":[{}]}]}`,
		`{"steps":[{"method":"GetStatus","request":null,"responses":[{}]}]}`,
		`{"steps":[{"method":"GetStatus","request":{},"responses":[null]}]}`,
		`{"steps":[{"method":"GetStatus","request":{},"failure":{"rpc_code":"synthetic-sensitive-marker","detail":{}}}]}`,
		`{"steps":[{"method":"GetStatus","request":{},"failure":{"rpc_code":"unavailable","detail":{}}}]}`,
	} {
		server, err := Load(strings.NewReader(raw))
		if err == nil || server != nil {
			t.Fatal("invalid script accepted")
		}
		if strings.Contains(err.Error(), "synthetic-sensitive-marker") {
			t.Fatal("script error disclosed input")
		}
	}
}
