package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"

	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestRPCProfileDriverFailsClosed(t *testing.T) {
	lock := &sync.Mutex{}
	driver := agentRPCProfileDriver(agentIPCOptions{OperationMu: lock})
	if driver.Lock != lock {
		t.Fatal("driver does not share agent lock")
	}
	_, err := driver.Stop(t.Context())
	if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal(err)
	}
	if err := driver.Start(t.Context(), client.Config{}); rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		t.Fatal(err)
	}
	wg := &testAgentWireGuard{}
	driver = agentRPCProfileDriver(agentIPCOptions{OperationMu: lock, WireGuard: wg})
	if err := driver.Start(t.Context(), client.Config{}); rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT || wg.configureCalls != 0 {
		t.Fatal("unverified map reached Configure", err)
	}
	continuity, err := driver.Stop(t.Context())
	if err != nil || continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN || wg.downCalls != 1 {
		t.Fatal("unknown inspection claimed continuity", err)
	}
	wg.down = func() (client.WireGuardApplyResult, error) {
		return client.WireGuardApplyResult{}, errors.New("private driver diagnostic")
	}
	_, err = driver.Stop(t.Context())
	if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED {
		t.Fatal(err)
	}
}

func TestRPCProfileStopNotifiesOfflineAfterTeardown(t *testing.T) {
	for _, mode := range []string{"notification", "timeout", "cancelled", "failed-teardown"} {
		t.Run(mode, func(t *testing.T) {
			var stopped atomic.Bool
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var request api.UpdateNodeEndpointRequest
				if !stopped.Load() || r.Method != http.MethodPatch || json.NewDecoder(r.Body).Decode(&request) != nil || request.Status != api.NodeStatusOffline {
					t.Error("offline notification preceded teardown or used an invalid request")
				}
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				// A rejected remote notification must not undo local disconnection.
				w.WriteHeader(http.StatusForbidden)
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			store, err := client.OpenConfigStore(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Read()
			wg := &testAgentWireGuard{down: func() (client.WireGuardApplyResult, error) {
				if mode == "failed-teardown" {
					return client.WireGuardApplyResult{}, errors.New("synthetic teardown failure")
				}
				stopped.Store(true)
				return client.WireGuardApplyResult{OK: true}, nil
			}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			driver := agentRPCProfileDriver(agentIPCOptions{ConfigStore: store, WireGuard: wg})
			started := time.Now()
			_, err = driver.Stop(ctx)
			if time.Since(started) > 5*time.Second {
				t.Fatal("offline notification exceeded its bounded wait")
			}
			if (err != nil) != (mode == "failed-teardown") {
				t.Fatal("remote notification changed local teardown outcome")
			}
			if (requests.Load() > 0) != (mode == "notification" || mode == "timeout") {
				t.Fatal("unexpected offline notification admission")
			}
			if !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("offline notification overwrote persisted configuration")
			}
		})
	}
}
