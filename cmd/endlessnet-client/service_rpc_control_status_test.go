package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNativeStatusControlAvailabilityAndFailover(t *testing.T) {
	for _, mode := range []string{"ready", "unavailable", "failover"} {
		t.Run(mode, func(t *testing.T) {
			var primaryCalls, secondaryCalls atomic.Int32
			handler := func(counter *atomic.Int32, code int) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					counter.Add(1)
					if r.Method != http.MethodGet || r.URL.Path != "/client/readyz" || r.Header.Get("Authorization") != "" || r.Header.Get("X-EndlessNet-Node-Credential") != "" {
						t.Error("readiness probe sent an invalid request or credentials")
					}
					w.WriteHeader(code)
					_, _ = w.Write([]byte("synthetic-private-server-diagnostic"))
				}
			}
			primaryCode := http.StatusServiceUnavailable
			if mode == "ready" {
				primaryCode = http.StatusOK
			}
			primary := httptest.NewServer(handler(&primaryCalls, primaryCode))
			defer primary.Close()
			secondary := httptest.NewServer(handler(&secondaryCalls, http.StatusOK))
			defer secondary.Close()
			networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
			cfg := client.Config{ControlPlaneURLs: []string{primary.URL}, NodeID: "node-1", NetworkID: "net-1", MapRevision: 7, CachedMap: &networkMap,
				Token: "synthetic-session-secret", NodeCredential: "synthetic-node-secret",
				MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature))}
			if mode != "unavailable" {
				cfg.ControlPlaneURLs = append(cfg.ControlPlaneURLs, secondary.URL)
			}
			status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED)
			ready := mode != "unavailable"
			if status.GetControl().GetOk() != ready || status.GetStoredState().GetCachedMapValid() != true || primaryCalls.Load() != 1 {
				t.Fatal("native control status lost probe or verified-cache evidence")
			}
			if ready {
				if status.ControlState != ipc.ControlState_CONTROL_STATE_READY || status.ServiceState != ipc.ServiceState_SERVICE_STATE_CONNECTED || status.Control.HttpStatus != http.StatusOK {
					t.Fatal("successful readiness was not projected")
				}
			} else if status.ControlState != ipc.ControlState_CONTROL_STATE_DEGRADED || status.ServiceState != ipc.ServiceState_SERVICE_STATE_DEGRADED || status.Control.HttpStatus != http.StatusServiceUnavailable || status.Control.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
				t.Fatal("failed readiness did not degrade native status")
			}
			if mode == "failover" {
				if secondaryCalls.Load() != 1 || len(status.Control.Attempts) != 2 || status.Control.Origin != secondary.URL ||
					status.Control.Attempts[0].HttpStatus != http.StatusServiceUnavailable || status.Control.Attempts[1].HttpStatus != http.StatusOK {
					t.Fatal("native control failover lost ordered attempts")
				}
			} else if secondaryCalls.Load() != 0 {
				t.Fatal("successful primary did not stop probing")
			}
			encoded, err := protojson.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			for _, withheld := range []string{cfg.Token, cfg.NodeCredential, "synthetic-private-server-diagnostic"} {
				if strings.Contains(string(encoded), withheld) {
					t.Fatal("native status disclosed credential or server response body")
				}
			}
		})
	}
}
