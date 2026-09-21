package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type underlayHTTPTestBody struct {
	read   func([]byte) (int, error)
	reads  atomic.Int32
	closes atomic.Int32
}

func (b *underlayHTTPTestBody) Read(p []byte) (int, error) { b.reads.Add(1); return b.read(p) }
func (b *underlayHTTPTestBody) Close() error               { b.closes.Add(1); return nil }

func TestUnderlayHTTPRequestLifetimeBinding(t *testing.T) {
	for _, scenario := range []string{"lease_revoked", "caller_cancelled", "cleanup", "already_revoked", "no_lease"} {
		t.Run(scenario, func(t *testing.T) {
			lease := newUnderlayDNSLease(func(context.Context) error { return nil })
			defer lease.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://control.example/map", nil)
			if err != nil {
				t.Fatal(err)
			}
			source := lease
			if scenario == "no_lease" {
				source = nil
			}
			if scenario == "already_revoked" {
				lease.Close()
			}
			bound, cleanup, err := bindUnderlayHTTPRequest(request, source)
			if scenario == "already_revoked" {
				if !errors.Is(err, context.Canceled) || bound != nil {
					t.Fatal("revoked lease admitted request", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if scenario == "no_lease" {
				if bound != request {
					t.Fatal("ordinary request was modified")
				}
				return
			}
			if bound == request || request.Context() != ctx {
				t.Fatal("original request was mutated")
			}
			switch scenario {
			case "lease_revoked":
				lease.Close()
			case "caller_cancelled":
				cancel()
			case "cleanup":
				cleanup()
				cleanup()
			}
			select {
			case <-bound.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("request cancellation was not propagated")
			}
			if scenario != "caller_cancelled" && ctx.Err() != nil {
				t.Fatal("binding cancelled caller context")
			}
		})
	}
}

func TestUnderlayHTTPBodyRejectsRevocationDuringRead(t *testing.T) {
	for _, partialErr := range []error{nil, io.EOF, errors.New("reader failed")} {
		lease := newUnderlayDNSLease(func(context.Context) error { return nil })
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://control.example/map", nil)
		if err != nil {
			t.Fatal(err)
		}
		bound, cleanup, err := bindUnderlayHTTPRequest(request, lease)
		if err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		underlying := &underlayHTTPTestBody{read: func(p []byte) (int, error) {
			close(entered)
			<-bound.Context().Done()
			return copy(p, "untrusted"), partialErr
		}}
		var cleanups atomic.Int32
		body := wrapUnderlayHTTPBody(underlying, lease, func() { cleanups.Add(1); cleanup() })
		result := make(chan error, 1)
		go func() {
			buf := make([]byte, 16)
			n, err := body.Read(buf)
			if n != 0 || !errors.Is(err, context.Canceled) || strings.Contains(string(buf), "untrusted") {
				result <- errors.New("revoked partial read delivered bytes")
				return
			}
			result <- nil
		}()
		<-entered
		lease.Close()
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("stream read remained blocked after revocation")
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
		if cleanups.Load() != 1 || underlying.closes.Load() != 1 {
			t.Fatal("revocation cleanup did not run exactly once")
		}
	}
}

func TestUnderlayHTTPBodyCleanupAndPureReads(t *testing.T) {
	for _, scenario := range []string{"eof", "read_error", "close", "revoked_before_read"} {
		t.Run(scenario, func(t *testing.T) {
			lease := newUnderlayDNSLease(func(context.Context) error { t.Error("body read performed native source observation"); return nil })
			defer lease.Close()
			readErr := io.EOF
			if scenario == "read_error" {
				readErr = errors.New("read failed")
			}
			underlying := &underlayHTTPTestBody{read: func(p []byte) (int, error) { return copy(p, "ok"), readErr }}
			cleanups := 0
			body := wrapUnderlayHTTPBody(underlying, lease, func() { cleanups++ })
			if scenario == "revoked_before_read" {
				lease.Close()
			}
			if scenario != "close" {
				n, err := body.Read(make([]byte, 8))
				if scenario == "revoked_before_read" {
					if n != 0 || !errors.Is(err, context.Canceled) || underlying.reads.Load() != 0 {
						t.Fatal("revoked stream invoked reader")
					}
				} else if n != 2 || err != readErr {
					t.Fatal("ordinary partial error was changed", n, err)
				}
				if cleanups != 1 {
					t.Fatal("read completion retained request binding")
				}
			}
			_ = body.Close()
			_ = body.Close()
			if cleanups != 1 || underlying.closes.Load() != 1 {
				t.Fatal("body cleanup repeated")
			}
		})
	}
}
