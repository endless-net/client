package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestRPCOfflineNotificationRequiresOwnedTransport(t *testing.T) {
	for _, scenario := range []string{"owned", "refused", "nil_client", "nil_transport", "late_cancel", "body_cancel", "missing_engine"} {
		t.Run(scenario, func(t *testing.T) {
			var plainRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { plainRequests.Add(1); w.WriteHeader(http.StatusOK) }))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			store, err := client.OpenConfigStore(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Read()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var stopped, bodyClosed bool
			var factoryCalls, transportCalls int
			transport := &rpcProbeTestTransport{roundTrip: func(r *http.Request) (*http.Response, error) {
				transportCalls++
				if _, bounded := r.Context().Deadline(); !bounded {
					t.Fatal("notification request lost its deadline")
				}
				var request api.UpdateNodeEndpointRequest
				if !stopped || r.Method != http.MethodPatch || r.URL.Path != "/nodes/"+before.NodeID+"/endpoint" || r.Header.Get("X-EndlessNet-Node-Credential") != before.NodeCredential || json.NewDecoder(r.Body).Decode(&request) != nil || request.Status != api.NodeStatusOffline {
					t.Fatal("notification lost authenticated target or preceded Down")
				}
				body := &rpcOfflineTestBody{close: func() { bodyClosed = true }, read: func([]byte) (int, error) {
					if scenario == "body_cancel" {
						cancel()
						<-r.Context().Done()
						return 0, r.Context().Err()
					}
					return 0, io.EOF
				}}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}, nil
			}}
			engine := &rpcProbeTestEngine{factory: func(cfg client.Config) (*http.Client, error) {
				factoryCalls++
				if !stopped || !reflect.DeepEqual(cfg, before) {
					t.Fatal("factory did not receive stopped runtime identity")
				}
				switch scenario {
				case "refused":
					return nil, errors.New("private protected transport refusal")
				case "nil_client":
					return nil, nil
				case "nil_transport":
					return &http.Client{}, nil
				case "late_cancel":
					cancel()
				}
				return &http.Client{Transport: transport}, nil
			}}
			engine.down = func() (client.WireGuardApplyResult, error) {
				stopped = true
				return client.WireGuardApplyResult{OK: true}, nil
			}
			opts := agentIPCOptions{ConfigStore: store, WireGuard: engine}
			if scenario == "missing_engine" {
				opts.WireGuard = nil
				notifyRPCNodeOffline(ctx, opts)
			} else if _, err := agentRPCProfileDriver(opts).Stop(ctx); err != nil || engine.downCalls != 1 {
				t.Fatal("unavailable offline notification changed successful Down", err)
			}
			wantFactory, wantTransport := 1, 0
			if scenario == "missing_engine" {
				wantFactory = 0
			}
			if scenario == "owned" || scenario == "body_cancel" {
				wantTransport = 1
			}
			if plainRequests.Load() != 0 || factoryCalls != wantFactory || transportCalls != wantTransport || !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("offline notification bypassed owned transport or mutated durable state")
			}
			if wantTransport == 1 && (!bodyClosed || !transport.closed) {
				t.Fatal("notification leaked body or idle transport")
			}
			if scenario == "late_cancel" && !transport.closed {
				t.Fatal("late cancellation leaked owned transport")
			}
		})
	}
}

type rpcOfflineTestBody struct {
	read  func([]byte) (int, error)
	close func()
}

func (b *rpcOfflineTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b *rpcOfflineTestBody) Close() error               { b.close(); return nil }
