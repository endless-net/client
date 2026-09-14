package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
)

type sessionHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f sessionHTTPClientFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

type sessionTransportFixture struct {
	clientrpcconnect.UnimplementedSessionServiceHandler
	t         *testing.T
	requestID string
}

func (f sessionTransportFixture) RenewSession(_ context.Context, request *connect.Request[backend.RenewSessionRequest]) (*connect.Response[backend.RenewSessionResponse], error) {
	if request.Header().Get("Authorization") != "Bearer "+strings.Repeat("renew-", 8) || request.Msg.RequestId != f.requestID || request.Msg.ExpectedSessionId != "session" {
		f.t.Error("renewal transport changed authority or identity")
	}
	return connect.NewResponse(&backend.RenewSessionResponse{Operation: &backend.SessionRenewal{OperationId: "operation"}, PollAuthorization: strings.Repeat("poll-", 8)}), nil
}

func (f sessionTransportFixture) GetSessionRenewal(_ context.Context, request *connect.Request[backend.GetSessionRenewalRequest]) (*connect.Response[backend.GetSessionRenewalResponse], error) {
	if request.Header().Get("Authorization") != "Bearer "+strings.Repeat("poll-", 8) || request.Msg.OperationId != "operation" {
		f.t.Error("polling transport changed authority or identity")
	}
	return connect.NewResponse(&backend.GetSessionRenewalResponse{Operation: &backend.SessionRenewal{OperationId: "operation"}}), nil
}

// Exercise generated serialization without opening a socket or a real backend.
func TestAgentSessionRenewalTransportUsesDedicatedAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	_, handler := clientrpcconnect.NewSessionServiceHandler(sessionTransportFixture{t: t, requestID: id})
	calls := 0
	provider := agentRPCSessionRenewalWithHTTPClient(sessionHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Scheme != "https" || request.URL.Host != "control.test" || request.Header.Get("Cookie") != "" {
			t.Error("unexpected origin or cookie fallback")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	}))
	request := &backend.RenewSessionRequest{RequestId: id, ExpectedSessionId: "session"}
	response, err := provider.Renew(t.Context(), "https://control.test", strings.Repeat("renew-", 8), request)
	if err != nil || response.GetOperation().GetOperationId() != "operation" {
		t.Fatal("renewal transport failed", err)
	}
	polled, err := provider.Poll(t.Context(), "https://control.test", response.PollAuthorization, &backend.GetSessionRenewalRequest{OperationId: "operation"})
	if err != nil || polled.GetOperation().GetOperationId() != "operation" {
		t.Fatal("poll transport failed", err)
	}
	for _, origin := range []string{"http://control.test", "https://user:pass@control.test", "https://control.test/path"} {
		if _, err := provider.Renew(t.Context(), origin, strings.Repeat("renew-", 8), request); err == nil {
			t.Error("invalid origin accepted", origin)
		}
	}
	if _, err := provider.Poll(t.Context(), "https://control.test", "", &backend.GetSessionRenewalRequest{OperationId: "operation"}); err == nil {
		t.Error("empty poll authority accepted")
	}
	if _, err := provider.Renew(t.Context(), "https://control.test", strings.Repeat("renew-", 8), &backend.RenewSessionRequest{}); err == nil {
		t.Error("invalid renewal identity accepted")
	}
	if calls != 2 {
		t.Fatal("invalid inputs reached transport")
	}
}
