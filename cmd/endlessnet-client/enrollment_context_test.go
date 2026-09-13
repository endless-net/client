package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

type enrollmentRoundTripFunc func(*http.Request) (*http.Response, error)

func (f enrollmentRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEnrollmentContextSurvivesHeadersUntilBodyConsumed(t *testing.T) {
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	requestCtx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://control.test/example", nil)
	if err != nil {
		t.Fatal(err)
	}
	var observed context.Context
	transport := enrollmentContextTransport{lifetime: lifetime, base: enrollmentRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		observed = r.Context()
		originalDeadline, _ := requestCtx.Deadline()
		deadline, _ := observed.Deadline()
		if !deadline.Equal(originalDeadline) {
			t.Fatal("request deadline lost")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("response-body"))}, nil
	})}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Err() != nil {
		t.Fatal("request cancelled at headers")
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil || string(raw) != "response-body" {
		t.Fatal("response was not readable", err)
	}
	if observed.Err() == nil {
		t.Fatal("consumed body retained cancellation registration")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentContextCancelsInflightRequest(t *testing.T) {
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	transport := enrollmentContextTransport{lifetime: lifetime, base: enrollmentRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://control.test/example", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := transport.RoundTrip(request); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not stop")
	}
}

func TestCancelledTypedEnrollmentHasNoLocalSideEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := enrollConfiguredClient(ctx, client.Config{}, clientEnrollmentOptions{Save: func(updated client.Config) error { return client.SaveConfig(path, updated) }})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled enrollment wrote config")
	}
}
