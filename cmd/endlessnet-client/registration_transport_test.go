package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestRegistrationRejectionRequiresCompleteContract(t *testing.T) {
	for _, tc := range []struct {
		name, path, contentType, requestID string
		status                             int
		code                               api.ErrorCode
		want                               bool
	}{
		{"denied", "/nodes/register", "application/json", "request-1", 403, api.ErrorCodeAuthorizationDenied, true},
		{"wrong-path", "/server-key", "application/json", "request-1", 403, api.ErrorCodeAuthorizationDenied, false},
		{"wrong-type", "/nodes/register", "text/plain", "request-1", 403, api.ErrorCodeAuthorizationDenied, false},
		{"missing-request-id", "/nodes/register", "application/json", "", 403, api.ErrorCodeAuthorizationDenied, false},
		{"mismatched-request-id", "/nodes/register", "application/json", "request-2", 403, api.ErrorCodeAuthorizationDenied, false},
		{"wrong-status", "/nodes/register", "application/json", "request-1", 503, api.ErrorCodeAuthorizationDenied, false},
		{"temporary", "/nodes/register", "application/json", "request-1", 503, api.ErrorCodeTemporarilyUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set(controlRequestIDHeader, tc.requestID)
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: tc.code, RequestID: "request-1", DiagnosticMessage: "test rejection"})
			}))
			defer s.Close()
			transport := &registrationAttemptTransport{denied: true}
			request, err := http.NewRequest(http.MethodPost, s.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			var public api.PublicError
			if err != nil || json.Unmarshal(body, &public) != nil || public.ErrorCode != tc.code {
				t.Fatal("response inspection changed the SDK-visible body")
			}
			if transport.denied != tc.want {
				t.Fatal("incorrect registration rejection classification")
			}
		})
	}
}
