package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
	"github.com/endless-net/client/internal/testcontrol"
)

// Component coverage of cache selection and validation. Native OS tests prove
// actual WireGuard configuration and traffic without a control endpoint.
func TestAgentCachedBootstrapWithoutControl(t *testing.T) {
	for _, scenario := range []string{"valid", "modified-map", "expired-cache", "missing-credential"} {
		t.Run(scenario, func(t *testing.T) {
			setInstallationStateDirForTest(t, t.TempDir())
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("cached-bootstrap", "100.95.0.0/24")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if _, err := captureStdout(t, func() error {
				return cmdUp([]string{"--config", path, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "cached-node"})
			}); err != nil {
				t.Fatal("fixture enrollment failed")
			}
			cfg, err := client.LoadConfig(path)
			if err != nil {
				t.Fatal("fixture configuration unavailable")
			}
			switch scenario {
			case "modified-map":
				cfg.CachedMap.Node.Hostname = "unsigned-change"
			case "expired-cache":
				expired := time.Now().Add(-2 * time.Hour)
				cfg.CachedMapSavedAt = &expired
			case "missing-credential":
				cfg.NodeCredential = ""
			}
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal("fixture configuration update failed")
			}
			s.SetUnavailable(true)
			if scenario == "valid" {
				if _, _, _, err := agentOnlineNetworkMap(path, 100*time.Millisecond, 0); err == nil {
					t.Fatal("online sync unexpectedly succeeded without control")
				}
			}
			before := len(s.Events())
			snapshot, err := runAgentCachedBootstrap(t.Context(), agentIterationOptions{
				ConfigPath: path, StateOutput: filepath.Join(t.TempDir(), "state.json"), MaxCacheAge: time.Hour,
			})
			if scenario == "valid" {
				if err != nil || snapshot.NodeID != cfg.NodeID || snapshot.MapRevision != cfg.MapRevision {
					t.Fatal("valid cached bootstrap did not retain enrolled identity and revision")
				}
			} else if err == nil {
				t.Fatal("invalid cached bootstrap was accepted")
			}
			if len(s.Events()) != before {
				t.Fatal("cached bootstrap made a control request")
			}
		})
	}
}
