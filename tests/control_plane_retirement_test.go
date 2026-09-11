package tests

import (
	"net"
	"os"
	"testing"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-065 / IT-20: a terminal credential response must remove actual access,
// including after restart. Observe public IPC and application traffic only.
func exerciseCredentialRetirement(t *testing.T, s *testcontrol.Server, nodes [2]*testclient.Node, initial [2]ipc.StatusResponse) {
	t.Helper()
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	flows := []struct{ protocol, port string }{{"tcp", "24001"}, {"udp", "24002"}}
	var sessions []func(string)
	for _, flow := range flows {
		session := startApplicationSession(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort(initial[1].OverlayIP, flow.port))
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
	if err := s.Revoke(initial[0].NodeID); err != nil {
		t.Fatal(err)
	}
	for phase := range 2 {
		if phase == 1 {
			nodes[0].Stop()
			nodes[0].Start()
		}
		nodes[0].AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && !v.NodeCredentialPresent && !v.CachedMapPresent
		})
		// Retain the receiver's original peer map: withdrawing its peer or
		// stopping its application cannot account for the revoked sender's denial.
		nodes[1].AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == initial[1].NodeID && v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.WireGuard.PeerCount == 1
		})
		for i, flow := range flows {
			sessions[i]("blocked")
			if !applicationProbe(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort("192.0.2.3", flow.port)) {
				t.Fatal("retirement denial coincided with underlay application failure")
			}
			if applicationProbe(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort(initial[1].OverlayIP, flow.port)) {
				t.Fatal("retired identity still exchanged fresh overlay traffic")
			}
		}
		if requests() != before {
			t.Fatal("terminal retirement or restart attempted registration")
		}
	}
}
