package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestControlTransportRotationCancelsAndJoinsOldResponse(t *testing.T) {
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer old.Close()
	client := newRotatingControlClient(old.Client())
	defer client.replace(nil)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, old.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	next := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer next.Close()
	replaced := make(chan struct{})
	go func() { client.replace(next.Client()); close(replaced) }()
	read := make(chan error, 1)
	go func() { _, readErr := io.ReadAll(response.Body); read <- readErr }()
	select {
	case readErr := <-read:
		if readErr == nil {
			t.Fatal("old response was not cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old response did not stop")
	}
	select {
	case <-replaced:
		t.Fatal("replacement returned while old response was still owned")
	default:
	}
	_ = response.Body.Close()
	select {
	case <-replaced:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement did not join closed response")
	}
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, next.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal("replacement client was not used")
	}
}
