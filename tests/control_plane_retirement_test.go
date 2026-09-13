package tests

import (
	"context"
	"net"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

// HC-065 / IT-20: a terminal credential response must remove actual access,
// including after restart. Observe public IPC and application traffic only.
func exerciseCredentialRetirement(t *testing.T, s *testcontrol.Server, nodes [2]*testclient.Node) {
	t.Helper()
	binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	var initial [2]*ipc.Status
	for i, node := range nodes {
		initial[i] = node.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
				nativeOverlayAddress(v, false).IsValid() && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
	}
	receiverIP := nativeOverlayAddress(initial[1], false).String()
	flows := []struct{ protocol, port string }{{"tcp", "24001"}, {"udp", "24002"}}
	var sessions []func(string)
	for _, flow := range flows {
		session := startApplicationSession(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort(receiverIP, flow.port))
		session("ok")
		sessions = append(sessions, session)
	}
	requests := func() int {
		count := 0
		for _, event := range s.Events() {
			if event.Kind == "registration-request" {
				count++
			}
		}
		return count
	}
	before := requests()
	if err := s.Revoke(initial[0].NodeId); err != nil {
		t.Fatal(err)
	}
	for phase := range 2 {
		if phase == 1 {
			nodes[0].Stop()
			nodes[0].Start()
		}
		nodes[0].AwaitNativeStatus(func(v *ipc.Status) bool {
			return nativeEnrollmentAbsent(v) && v.ActiveProfileId == initial[0].ActiveProfileId
		})
		// Retain the receiver's original peer map: withdrawing its peer or
		// stopping its application cannot account for the revoked sender's denial.
		receiver := nodes[1].AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == initial[1].NodeId && v.ActiveProfileId == initial[1].ActiveProfileId && v.PeerCount == 1 && v.GetStoredState().GetCachedMapValid() &&
				v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && v.Agent.MapRevision == v.MapRevision && v.Agent.LastFailure == nil &&
				v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		err := testclient.Await(ctx, func() bool {
			response := &ipc.GetDiagnosticsResponse{}
			if nodes[1].NativeService("diagnostics", response, "--profile-id", receiver.ActiveProfileId, "--timeout", "1s") != nil {
				return false
			}
			d := response.GetDiagnostics()
			return d.GetStatus().GetNodeId() == receiver.NodeId && d.GetStatus().GetActiveProfileId() == receiver.ActiveProfileId && d.GetStatus().GetMapRevision() >= receiver.MapRevision &&
				d.GetTunnel().GetOk() && d.GetTunnel().GetFailure() == nil && len(d.GetTunnel().GetPeers()) == 1 && d.GetTunnel().GetPeers()[0].GetPeerId() == initial[0].NodeId
		})
		cancel()
		if err != nil {
			t.Fatal("receiver did not retain the retired sender's inspected tunnel peer")
		}
		for i, flow := range flows {
			sessions[i]("blocked")
			if !applicationProbe(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort("192.0.2.3", flow.port)) {
				t.Fatal("retirement denial coincided with underlay application failure")
			}
			if applicationProbe(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort(receiverIP, flow.port)) {
				t.Fatal("retired identity still exchanged fresh overlay traffic")
			}
		}
		if requests() != before {
			t.Fatal("terminal retirement or restart attempted registration")
		}
	}
}
