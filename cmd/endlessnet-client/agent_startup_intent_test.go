package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/endless-net/client/internal/client"
)

func TestCmdAgentWithoutSavedIntentDoesNotContactControlPlane(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node", NetworkID: "network", NodeCredential: "synthetic-credential"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := cmdAgent([]string{"--once", "--config", path, "--state-output", path + ".state", "--timeout", "1s"}); err != nil {
			t.Fatal(err)
		}
		cfg, err := client.LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 0 || cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected || cfg.ConnectionIntent.Reason != "runtime_start_policy_unavailable" {
			t.Fatal("agent implicitly connected without a saved intent", requests.Load())
		}
	}
}
