package main

import (
	"path/filepath"
	"testing"

	"github.com/endless-net/client/internal/testcontrol"
)

func TestConnectSyncPreservesEnrolledHostname(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("reconnect", "100.95.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "client.json")
	if _, err := captureStdout(t, func() error {
		return cmdUp([]string{"--config", config, "--server", s.URL(), "--network", network.Name, "--join-token", join, "--hostname", "enrolled-custom-hostname"})
	}); err != nil {
		t.Fatalf("initial enrollment failed: %v", err)
	}
	for range 2 {
		if _, err := captureStdout(t, func() error {
			return syncAgentForConnect(agentIPCOptions{ConfigPath: config})
		}); err != nil {
			t.Fatalf("connect sync failed to preserve the enrolled hostname: %v", err)
		}
	}
	var nodeID string
	registered, refreshed := 0, 0
	for _, event := range s.Events() {
		switch event.Kind {
		case "registered":
			registered++
			nodeID = event.NodeID
		case "registration-refreshed":
			refreshed++
			if event.NodeID != nodeID {
				t.Fatal("connect sync changed node identity")
			}
		}
	}
	if registered != 1 || refreshed != 2 {
		t.Fatal("connect must renew the existing enrollment")
	}
}
