package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewLocalHTTPServerAppliesBoundedPolicy(t *testing.T) {
	server := NewLocalHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.IdleTimeout != 120*time.Second {
		t.Fatalf("unexpected local HTTP timeouts: header=%s read=%s idle=%s", server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 64<<10 {
		t.Fatalf("local HTTP max header bytes = %d", server.MaxHeaderBytes)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/"+strings.Repeat("a", localHTTPMaxPathBytes+1), nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestURITooLong {
		t.Fatalf("oversized request status = %d", response.Code)
	}
}
