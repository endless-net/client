package publicationproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWithdrawalWaitsForServingRequestBeforeAck(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	closed := make(chan struct{})
	g := routeGate{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		<-closed
	})}
	request := func() int {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/", nil))
		return w.Code
	}
	if request() != 404 {
		t.Fatal("served before first snapshot")
	}
	if err := g.replace(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	go request()
	<-entered
	ack := make(chan struct{})
	replaced := make(chan error, 1)
	go func() { replaced <- g.replace(t.Context(), func() error { close(ack); return nil }) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
	if request() != 404 {
		t.Fatal("served during withdrawal")
	}
	select {
	case <-ack:
		t.Fatal("ack before old request exited")
	default:
	}
	close(closed)
	select {
	case <-ack:
	case <-time.After(time.Second):
		t.Fatal("new epoch not installed")
	}
	// The production control loop waits for replace to return before shutdown.
	// The callback above acknowledges the drained request before that return.
	if err := <-replaced; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := g.replace(ctx, nil); err == nil || request() != 404 {
		t.Fatal("shutdown admitted request")
	}
}
