package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	for _, mode := range []string{"success", "notification", "timeout", "cancelled", "failed-teardown"} {
		t.Run(mode, func(t *testing.T) {
			var stopped atomic.Bool
			var requests atomic.Int32
			var before client.Config
			var fixture recoveryTestFixture
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var request api.UpdateNodeEndpointRequest
				if !stopped.Load() || r.Method != http.MethodPatch || r.URL.Path != "/nodes/"+before.NodeID+"/endpoint" ||
					r.Header.Get("X-EndlessNet-Node-Credential") != before.NodeCredential ||
					json.NewDecoder(r.Body).Decode(&request) != nil || request.Status != api.NodeStatusOffline || request.ClientVersion != version {
					t.Error("offline notification preceded teardown or used an invalid request")
				}
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				if mode == "success" {
					offlineMap := *before.CachedMap
					offlineMap.Node.Status = api.NodeStatusOffline
					signature, err := api.SignNetworkMap(fixture.OldSigningKey, offlineMap)
					if err != nil {
						t.Error("cannot sign synthetic offline reply")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					offlineMap.MapSignature = signature
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(offlineMap); err != nil {
						t.Error("cannot encode synthetic offline reply")
					}
					return
				}
				// A rejected remote notification must not undo local disconnection.
				w.WriteHeader(http.StatusForbidden)
			}))
			defer server.Close()
			fixture = newRecoveryTestFixture(t, server.URL)
			store, err := client.OpenConfigStore(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			before = store.Read()
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
			var wantRequests int32
			if mode == "success" || mode == "notification" || mode == "timeout" {
				wantRequests = 1
			}
			if requests.Load() != wantRequests {
				t.Fatal("unexpected offline notification admission")
			}
			if !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("offline notification overwrote persisted configuration")
			}
		})
	}
}

func TestRPCProfileStartRefreshesAgentOnlyAfterSuccessfulApply(t *testing.T) {
	for _, mode := range []string{"success", "wake-already-pending", "configure-error", "configure-rejected", "unverified-map"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newRecoveryTestFixture(t, "https://control.example.test")
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg.EnrollmentRecovery = nil
			cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
			if mode == "unverified-map" {
				cfg.MapSigningTrust = &fixture.NewTrust
			}
			statePath := filepath.Join(t.TempDir(), "agent-state.json")
			if err := writeAgentFailureSnapshot(statePath, fixture.ConfigPath, errors.New("synthetic previous control failure")); err != nil {
				t.Fatal(err)
			}
			wake := make(chan struct{}, 1)
			if mode == "wake-already-pending" {
				wake <- struct{}{}
			}
			wg := &testAgentWireGuard{configure: func(client.Config, api.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
				if mode == "configure-error" {
					return client.WireGuardApplyResult{}, errors.New("synthetic apply error")
				}
				return client.WireGuardApplyResult{OK: mode != "configure-rejected"}, nil
			}}
			driver := agentRPCProfileDriver(agentIPCOptions{WireGuard: wg, StateOutput: statePath, SyncWake: wake})
			err = driver.Start(t.Context(), cfg)
			succeeded := mode == "success" || mode == "wake-already-pending"
			if (err == nil) != succeeded {
				t.Fatal("unexpected native start outcome", err)
			}
			_, statErr := os.Stat(statePath)
			if succeeded {
				if !os.IsNotExist(statErr) || len(wake) != 1 {
					t.Fatal("successful native start retained stale state or did not wake agent")
				}
			} else if statErr != nil || len(wake) != 0 {
				t.Fatal("failed native start invalidated state or woke agent")
			}
			wantCalls := 1
			if mode == "unverified-map" {
				wantCalls = 0
			}
			if wg.configureCalls != wantCalls {
				t.Fatal("unexpected Configure call count")
			}
		})
	}
}
