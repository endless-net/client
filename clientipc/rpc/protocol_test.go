package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	clientipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
)

func TestEveryMethodHasAccess(t *testing.T) {
	methods := clientipc.File_client_v0_service_proto.Services().ByName("ClientService").Methods()
	for i := 0; i < methods.Len(); i++ {
		name := string(methods.Get(i).Name())
		access, ok := RequiredAccess("/client.v0.ClientService/" + name)
		if !ok || access == clientipc.Access_ACCESS_UNSPECIFIED {
			t.Errorf("method %s has no enforceable access", name)
		}
	}
	for _, path := range []string{"/status", "/client.v2.ClientService/GetStatus", "/client.v0.ClientService/Unknown", "/client.v0.ClientService/GetStatus/extra"} {
		if _, ok := RequiredAccess(path); ok {
			t.Errorf("accepted unknown procedure %q", path)
		}
	}
}

func TestGuardMetadataAndAuthorizationOrder(t *testing.T) {
	procedure := clientipcconnect.ClientServiceGetStatusProcedure
	valid := make(http.Header)
	SetHeaders(valid)
	guard := Guard{Authorize: func(_ context.Context, access clientipc.Access, got string) error {
		if access != clientipc.Access_ACCESS_OBSERVER || got != procedure {
			t.Fatalf("wrong authorization: %v %q", access, got)
		}
		return nil
	}}
	if err := guard.check(t.Context(), procedure, valid); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{ProtocolHeader, VersionHeader, DigestHeader} {
		for _, kind := range []string{"missing", "wrong", "duplicate"} {
			t.Run(key+"/"+kind, func(t *testing.T) {
				h := valid.Clone()
				switch kind {
				case "missing":
					h.Del(key)
				case "wrong":
					h.Set(key, "wrong")
				case "duplicate":
					h.Add(key, h.Get(key))
				}
				err := guard.check(t.Context(), procedure, h)
				if connect.CodeOf(err) != connect.CodeFailedPrecondition || FailureFromError(err).GetCode() != clientipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH {
					t.Fatalf("expected typed mismatch, got %v", err)
				}
			})
		}
	}
	for _, guard := range []Guard{{}, {Authorize: func(context.Context, clientipc.Access, string) error {
		return Error(connect.CodeUnauthenticated, clientipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}}} {
		for _, procedure := range []string{procedure, clientipcconnect.ClientServiceGetRuntimeInfoProcedure} {
			if err := guard.check(t.Context(), procedure, nil); connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("metadata/bootstrap bypassed authentication: %v", err)
			}
		}
	}
	bootstrap := Guard{Authorize: func(context.Context, clientipc.Access, string) error { return nil }}
	if err := bootstrap.check(t.Context(), clientipcconnect.ClientServiceGetRuntimeInfoProcedure, nil); err != nil {
		t.Fatalf("authenticated bootstrap must diagnose mismatched versions: %v", err)
	}
}

type unreachableService struct {
	clientipcconnect.UnimplementedClientServiceHandler
}

func TestGuardTypedFailureOverGRPC(t *testing.T) {
	_, handler := clientipcconnect.NewClientServiceHandler(unreachableService{}, connect.WithInterceptors(Guard{
		Authorize: func(context.Context, clientipc.Access, string) error { return nil },
	}))
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := clientipcconnect.NewClientServiceClient(server.Client(), server.URL, connect.WithGRPC())
	_, err := client.GetStatus(t.Context(), connect.NewRequest(&clientipc.GetStatusRequest{}))
	assertMismatch := func(err error) {
		t.Helper()
		if connect.CodeOf(err) != connect.CodeFailedPrecondition || FailureFromError(err).GetCode() != clientipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH {
			t.Fatalf("guard did not reject before invoking unimplemented handler: %v", err)
		}
	}
	assertMismatch(err)
	stream, err := client.WatchEvents(t.Context(), connect.NewRequest(&clientipc.WatchEventsRequest{}))
	if err != nil {
		assertMismatch(err)
		return
	}
	defer func() { _ = stream.Close() }()
	if stream.Receive() {
		t.Fatal("rejected stream produced an event")
	}
	assertMismatch(stream.Err())
}
