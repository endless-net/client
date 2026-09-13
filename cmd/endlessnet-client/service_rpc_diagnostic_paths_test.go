package main

import (
	"math"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeDiagnosticPathsProjectsActualMonitorValues(t *testing.T) {
	peers := []*ipc.Peer{{Id: "a", Hostname: "verified-host"}}
	path := client.PeerPathStatus{PeerID: "a", Hostname: "untrusted-host", SelectedPath: "direct", SelectedEndpoint: "192.0.2.1:1234", SelectionReason: "private raw reason", LastTransitionAt: "2026-09-13T00:00:00Z",
		Candidates: []client.PathCandidateStatus{{Type: "direct", State: "reachable", Endpoint: "192.0.2.1:1234", RTTMS: 1.5, CheckedAt: "2026-09-13T00:00:01Z", Reason: "private probe detail"}}}
	if failures := nativeDiagnosticPaths(peers, []client.PeerPathStatus{path}); len(failures) != 0 {
		t.Fatal("valid monitor observation rejected")
	}
	peer := peers[0]
	if peer.Hostname != "verified-host" || peer.SelectedPath != ipc.PathKind_PATH_KIND_DIRECT || peer.SelectedEndpoint != path.SelectedEndpoint || peer.LastTransitionAt == nil {
		t.Fatal("identity or selection projection changed")
	}
	if len(peer.Candidates) != 1 || peer.Candidates[0].Health != ipc.PathHealth_PATH_HEALTH_REACHABLE || peer.Candidates[0].Rtt.Nanos != 1500000 || peer.Candidates[0].ReasonKey != "path_monitor_reachable" || peer.SelectionReasonKey != "path_monitor_selection" {
		t.Fatal("typed monitor fields or reason keys lost")
	}
	path.Candidates[0].Endpoint = "changed"
	if peer.Candidates[0].Endpoint != "192.0.2.1:1234" {
		t.Fatal("path output aliases input")
	}
}

func TestNativeDiagnosticPathsRejectInvalidObservation(t *testing.T) {
	for _, mode := range []string{"foreign-peer", "selected-kind", "health", "timestamp", "nan", "negative-priority", "negative-failures"} {
		t.Run(mode, func(t *testing.T) {
			peers := []*ipc.Peer{{Id: "a"}}
			path := client.PeerPathStatus{PeerID: "a", SelectedPath: "direct", Candidates: []client.PathCandidateStatus{{Type: "direct", State: "reachable"}}}
			switch mode {
			case "foreign-peer":
				path.PeerID = "other"
			case "selected-kind":
				path.SelectedPath = "invented"
			case "health":
				path.Candidates[0].State = "invented"
			case "timestamp":
				path.Candidates[0].CheckedAt = "invalid"
			case "nan":
				path.Candidates[0].RTTMS = math.NaN()
			case "negative-priority":
				path.Candidates[0].Priority = -1
			case "negative-failures":
				path.Candidates[0].ConsecutiveFailures = -1
			}
			if failures := nativeDiagnosticPaths(peers, []client.PeerPathStatus{path}); len(failures) != 1 {
				t.Fatal("invalid path not reported")
			}
			if len(peers[0].Candidates) != 0 || peers[0].SelectedPath != ipc.PathKind_PATH_KIND_UNSPECIFIED {
				t.Fatal("partial invalid path became visible")
			}
		})
	}
}
