package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func nativeTunnelHandshakeUnix(peer *ipc.TunnelPeer) int64 {
	stamp := peer.GetLatestHandshake()
	if stamp == nil || stamp.CheckValid() != nil {
		return 0
	}
	return max(0, stamp.AsTime().Unix())
}

func TestNativeTunnelHandshakeRequiresValidPositiveTimestamp(t *testing.T) {
	for _, peer := range []*ipc.TunnelPeer{
		nil, {}, {LatestHandshake: &timestamppb.Timestamp{}},
		{LatestHandshake: &timestamppb.Timestamp{Seconds: -1}},
		{LatestHandshake: &timestamppb.Timestamp{Seconds: 100, Nanos: 1000000000}},
	} {
		if nativeTunnelHandshakeUnix(peer) != 0 {
			t.Fatal("absent or invalid handshake became a positive observation")
		}
	}
	if nativeTunnelHandshakeUnix(&ipc.TunnelPeer{LatestHandshake: timestamppb.New(time.Unix(100, 0))}) != 100 {
		t.Fatal("valid native handshake timestamp lost")
	}
}

func nativePeerMapApplied(status, baseline *ipc.Status, revision uint64, count uint32) bool {
	return status != nil && baseline.GetNodeId() != "" && baseline.GetActiveProfileId() != "" &&
		status.NodeId == baseline.NodeId && status.ActiveProfileId == baseline.ActiveProfileId &&
		status.MapRevision >= revision && status.GetStoredState().GetCachedMapValid() && status.PeerCount == count &&
		status.Agent != nil && status.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT &&
		status.Agent.MapRevision == status.MapRevision && status.Agent.PeerCount == count && status.Agent.LastFailure == nil &&
		status.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
}

func awaitNativePeerMap(t *testing.T, node *testclient.Node, baseline *ipc.Status, revision uint64, count uint32) *ipc.Status {
	t.Helper()
	return node.AwaitNativeStatus(func(status *ipc.Status) bool {
		return nativePeerMapApplied(status, baseline, revision, count)
	})
}

func awaitNativePeerTunnel(t *testing.T, node *testclient.Node, baseline *ipc.Status, predicate func(*ipc.TunnelInspection) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var last *ipc.Diagnostics
	attempts, requestFailures := 0, 0
	if err := testclient.Await(ctx, func() bool {
		attempts++
		response := &ipc.GetDiagnosticsResponse{}
		if node.NativeService("diagnostics", response, "--profile-id", baseline.ActiveProfileId, "--timeout", "1s") != nil {
			requestFailures++
			return false
		}
		d := response.GetDiagnostics()
		last = d
		return d.GetStatus().GetNodeId() == baseline.NodeId && d.GetStatus().GetActiveProfileId() == baseline.ActiveProfileId &&
			d.GetStatus().GetMapRevision() >= baseline.MapRevision && d.GetTunnel().GetOk() && d.GetTunnel().GetFailure() == nil && predicate(d.Tunnel)
	}); err != nil {
		t.Fatalf("native diagnostics did not expose the expected profile/map-bound tunnel peers: attempts=%d request_failures=%d last={%s}", attempts, requestFailures, nativePeerTunnelSummary(last, baseline))
	}
}

// Report only bounded counts, booleans and numeric contract codes. Never dump
// diagnostics, peer identities, endpoints, public keys or arbitrary failure text.
func nativePeerTunnelSummary(d *ipc.Diagnostics, baseline *ipc.Status) string {
	status, tunnel := d.GetStatus(), d.GetTunnel()
	handshakes, received, transmitted := 0, 0, 0
	for _, peer := range tunnel.GetPeers() {
		if nativeTunnelHandshakeUnix(peer) > 0 {
			handshakes++
		}
		if peer.GetReceivedBytes() > 0 {
			received++
		}
		if peer.GetTransmittedBytes() > 0 {
			transmitted++
		}
	}
	return fmt.Sprintf("response=%t node_bound=%t profile_bound=%t map_current=%t tunnel_ok=%t tunnel_failure=%d diagnostic_failures=%d peers=%d handshakes=%d receiving=%d transmitting=%d",
		d != nil, baseline.GetNodeId() != "" && status.GetNodeId() == baseline.GetNodeId(),
		baseline.GetActiveProfileId() != "" && status.GetActiveProfileId() == baseline.GetActiveProfileId(),
		status != nil && baseline != nil && status.GetMapRevision() >= baseline.GetMapRevision(),
		tunnel.GetOk(), tunnel.GetFailure().GetCode(), len(d.GetFailures()), len(tunnel.GetPeers()), handshakes, received, transmitted)
}

func TestNativePeerMapRequiresCurrentAppliedObservation(t *testing.T) {
	baseline := &ipc.Status{NodeId: "node", ActiveProfileId: "profile"}
	valid := &ipc.Status{
		NodeId: "node", ActiveProfileId: "profile", MapRevision: 8, PeerCount: 1,
		StoredState:     &ipc.StoredStatePresence{CachedMapValid: true},
		Agent:           &ipc.AgentStatus{SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, MapRevision: 8, PeerCount: 1},
		ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED,
	}
	if !nativePeerMapApplied(valid, baseline, 8, 1) {
		t.Fatal("current applied peer map rejected")
	}
	for name, corrupt := range map[string]func(*ipc.Status){
		"foreign-node":            func(s *ipc.Status) { s.NodeId = "other" },
		"foreign-profile":         func(s *ipc.Status) { s.ActiveProfileId = "other" },
		"old-map":                 func(s *ipc.Status) { s.MapRevision = 7 },
		"missing-cache":           func(s *ipc.Status) { s.StoredState = nil },
		"wrong-peer-count":        func(s *ipc.Status) { s.PeerCount = 0 },
		"missing-agent":           func(s *ipc.Status) { s.Agent = nil },
		"previous-agent":          func(s *ipc.Status) { s.Agent.SnapshotState = ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS },
		"different-applied-map":   func(s *ipc.Status) { s.Agent.MapRevision = 7 },
		"different-applied-peers": func(s *ipc.Status) { s.Agent.PeerCount = 0 },
		"agent-failure":           func(s *ipc.Status) { s.Agent.LastFailure = &ipc.Failure{} },
		"unknown-phase":           func(s *ipc.Status) { s.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED },
	} {
		t.Run(name, func(t *testing.T) {
			status := proto.Clone(valid).(*ipc.Status)
			corrupt(status)
			if nativePeerMapApplied(status, baseline, 8, 1) {
				t.Fatal("incomplete or unrelated observation accepted")
			}
		})
	}
	if nativePeerMapApplied(nil, baseline, 8, 1) || nativePeerMapApplied(valid, nil, 8, 1) {
		t.Fatal("missing status or baseline accepted")
	}
}
