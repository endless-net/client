package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestHeadlessStatusUsesNativeJSONWithoutInferringConnection(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(map[bool]string{true: "ready", false: "unavailable"}[available], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !available {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg.EnrollmentRecovery = nil
			cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
			if err := client.SaveConfig(fixture.ConfigPath, cfg); err != nil {
				t.Fatal(err)
			}
			output, err := captureStdout(t, func() error { return cmdStatus([]string{"--config", fixture.ConfigPath, "--json"}) })
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal([]byte(output), &envelope); err != nil {
				t.Fatal(err)
			}
			status := new(ipc.Status)
			if err := protojson.Unmarshal(envelope["status"], status); err != nil {
				t.Fatal("headless status is not native protobuf JSON", err)
			}
			wantControl := ipc.ControlState_CONTROL_STATE_READY
			if !available {
				wantControl = ipc.ControlState_CONTROL_STATE_DEGRADED
			}
			if status.NodeId != cfg.NodeID || status.MapRevision != cfg.MapRevision || !status.GetStoredState().GetCachedMapValid() ||
				status.ControlState != wantControl || status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED ||
				status.ServiceState == ipc.ServiceState_SERVICE_STATE_CONNECTED || envelope["route_conflicts"] == nil || envelope["node_id"] != nil {
				t.Fatal("headless native status lost diagnostic facts or inferred connection")
			}
			for _, secret := range []string{cfg.PrivateKey, cfg.IdentityPrivateKey, cfg.NodeCredential} {
				if secret != "" && strings.Contains(output, secret) {
					t.Fatal("headless status disclosed local authority")
				}
			}
		})
	}
}
