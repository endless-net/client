package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

type rpcProbeTestEngine struct {
	testAgentWireGuard
	factory func(client.Config) (*http.Client, error)
}

func (e *rpcProbeTestEngine) ControlPlaneHTTPClient(cfg client.Config) (*http.Client, error) {
	if e.factory != nil {
		return e.factory(cfg)
	}
	return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}, nil
}

type rpcProbeTestTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
	closed    bool
}

func (t *rpcProbeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.roundTrip(r)
}
func (t *rpcProbeTestTransport) CloseIdleConnections() { t.closed = true }

func TestRPCControlProbeUsesOwnedTransport(t *testing.T) {
	cfg := client.Config{ControlPlaneURLs: []string{"https://control.example"}, NodeID: "node", NodeCredential: "synthetic-node-secret", Token: "synthetic-token"}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := url.Parse(cfg.ControlPlaneURLs[0])
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "session", Value: "synthetic-cookie"}})
	var calls int
	transport := &rpcProbeTestTransport{roundTrip: func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://control.example/client/readyz" || r.Method != http.MethodGet || len(r.Header) != 0 {
			t.Fatal("probe changed target or sent credentials/cookies")
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("probe lost request deadline")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private response")), Request: r}, nil
	}}
	owned := &http.Client{Transport: transport, Jar: jar}
	engine := &rpcProbeTestEngine{factory: func(got client.Config) (*http.Client, error) {
		if got.NodeID != cfg.NodeID || got.NodeCredential != cfg.NodeCredential {
			t.Fatal("factory lost runtime identity")
		}
		return owned, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	probe := probeAgentRPCControl(ctx, agentIPCOptions{WireGuard: engine}, cfg)
	if !probe.GetOk() || calls != 1 || !transport.closed || owned.Jar != jar || owned.CheckRedirect != nil {
		t.Fatal("probe bypassed or mutated owned client, or failed to close idle transport")
	}
}

func TestRPCControlProbeNeverFallsBackFromUnavailableTransport(t *testing.T) {
	for _, scenario := range []string{"missing_engine", "refused", "nil_client", "nil_transport", "cancelled", "transport_error"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusOK) }))
			defer server.Close()
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node"}
			var factoryCalls int
			transport := &rpcProbeTestTransport{roundTrip: func(*http.Request) (*http.Response, error) { return nil, errors.New("private transport failure") }}
			engine := &rpcProbeTestEngine{factory: func(client.Config) (*http.Client, error) {
				factoryCalls++
				switch scenario {
				case "refused":
					return nil, errors.New("private factory failure")
				case "nil_client":
					return nil, nil
				case "nil_transport":
					return &http.Client{}, nil
				default:
					return &http.Client{Transport: transport}, nil
				}
			}}
			opts := agentIPCOptions{WireGuard: engine}
			if scenario == "missing_engine" {
				opts.WireGuard = nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			probe := probeAgentRPCControl(ctx, opts, cfg)
			if probe.GetOk() || probe.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || requests.Load() != 0 {
				t.Fatal("unavailable protected transport fell back to ordinary network")
			}
			if (scenario == "cancelled" || scenario == "missing_engine") && factoryCalls != 0 {
				t.Fatal("unavailable probe invoked factory")
			}
			if scenario == "transport_error" && !transport.closed {
				t.Fatal("failed transport was not closed")
			}
			raw, err := protojson.Marshal(probe)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "private") {
				t.Fatal("probe disclosed transport diagnostic")
			}
		})
	}
}

type rpcProbeTestBody struct {
	read   func([]byte) (int, error)
	closed bool
}

func (b *rpcProbeTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b *rpcProbeTestBody) Close() error               { b.closed = true; return nil }

func TestRPCControlProbeRejectsIncompleteResponse(t *testing.T) {
	for _, scenario := range []string{"read_error", "cancelled_during_read"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var reads int
			body := &rpcProbeTestBody{read: func(p []byte) (int, error) {
				reads++
				n := copy(p, "private response payload")
				if scenario == "cancelled_during_read" {
					cancel()
					// A transport may return its buffered final bytes successfully
					// while cancellation is already visible to the caller.
					return n, io.EOF
				}
				return n, errors.New("private response read failure")
			}}
			transport := &rpcProbeTestTransport{roundTrip: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}, nil
			}}
			engine := &rpcProbeTestEngine{factory: func(client.Config) (*http.Client, error) { return &http.Client{Transport: transport}, nil }}
			probe := probeAgentRPCControl(ctx, agentIPCOptions{WireGuard: engine}, client.Config{ControlPlaneURLs: []string{"https://control.example"}})
			if probe.GetOk() || probe.GetHttpStatus() != http.StatusOK || probe.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || reads != 1 || !body.closed || !transport.closed {
				t.Fatal("incomplete or cancelled response became readiness, or leaked transport resources")
			}
			raw, err := protojson.Marshal(probe)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "private") {
				t.Fatal("probe disclosed response data or read error")
			}
		})
	}
}
