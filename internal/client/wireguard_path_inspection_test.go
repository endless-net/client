package client

import (
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestTryPathStatusBindsAppliedMapAndCopiesObservations(t *testing.T) {
	engine := &WireGuardEngine{configured: true,
		pathMap:    clientapi.RegisterNodeResponse{Network: clientapi.Network{ID: "network", Revision: 7}, Node: clientapi.Node{ID: "node"}},
		relayPaths: &wireGuardRelayPathManager{statuses: []PeerPathStatus{{PeerID: "peer", Candidates: []PathCandidateStatus{{Type: "direct", State: "reachable"}}}}}}
	for _, query := range []struct {
		network, node string
		revision      uint64
	}{{"other", "node", 7}, {"network", "other", 7}, {"network", "node", 8}, {"network", "node", 0}} {
		if paths, ok := engine.TryPathStatus(query.network, query.node, query.revision); ok || paths != nil {
			t.Fatal("paths crossed applied map identity")
		}
	}
	paths, ok := engine.TryPathStatus("network", "node", 7)
	if !ok || len(paths) != 1 {
		t.Fatal("matching path observation unavailable")
	}
	paths[0].Candidates[0].State = "changed"
	again, ok := engine.TryPathStatus("network", "node", 7)
	if !ok || again[0].Candidates[0].State != "reachable" {
		t.Fatal("path snapshot aliases manager")
	}
	engine.configured = false
	if _, ok := engine.TryPathStatus("network", "node", 7); ok {
		t.Fatal("disconnected engine exposed paths")
	}
}

func TestTryPathStatusDoesNotWaitForBusyEngine(t *testing.T) {
	engine := &WireGuardEngine{}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	done := make(chan bool, 1)
	go func() { _, ok := engine.TryPathStatus("network", "node", 7); done <- ok }()
	select {
	case available := <-done:
		if available {
			t.Fatal("busy engine reported path availability")
		}
	case <-time.After(time.Second):
		t.Fatal("path inspection blocked on engine mutation")
	}
}
