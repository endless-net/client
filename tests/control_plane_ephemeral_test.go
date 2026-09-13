package tests

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
)

// HC-011: a real Client exposes its provider-assigned ephemeral lifecycle,
// carries traffic during the job, retires local access after the provider's
// terminal cleanup response, and permits a fresh job identity to enroll.
func TestControlPlaneEphemeralLifecycle(t *testing.T) {
	exerciseEphemeralLifecycle(t)
}

func exerciseEphemeralLifecycle(t *testing.T) {
	t.Helper()
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("ephemeral", "198.18.90.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetJoinTokenEphemeral(token); err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, token, "--route-table", "auto")
	n.Start()
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.Ephemeral && v.GetStoredState().GetCachedMapValid() && nativeOverlayAddress(v, false).IsValid() && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	id := status.NodeId
	snapshot, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	clientIP := nativeOverlayAddress(status, false)
	peerIP := netip.MustParseAddr("198.18.90.20")
	underlay := nativePeerUnderlay(t, clientIP, peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{ID: "ephemeral-peer", Hostname: "ephemeral-peer", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{peerIP.String() + "/32"}}
	if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) { m.Peers = []api.Peer{peer} }); err != nil {
		t.Fatal(err)
	}
	current, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	status = n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.Ephemeral && v.MapRevision >= current.Revision.Network && v.PeerCount == 1 && v.GetStoredState().GetCachedMapValid() &&
			v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && v.Agent.MapRevision == v.MapRevision && v.Agent.LastFailure == nil && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, n, status)))
	probe := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	if err := testclient.Await(ctx, func() bool {
		beforeReceived, beforeEchoed := reference.PacketCounts()
		if !applicationProbe(t, probe, "", "tcp", net.JoinHostPort(peerIP.String(), "24001")) {
			return false
		}
		received, echoed := reference.PacketCounts()
		return received > beforeReceived && echoed > beforeEchoed
	}); err != nil {
		cancel()
		t.Fatal("ephemeral client could not carry application traffic")
	}
	cancel()

	// The provider owns absence detection and cleanup timing. Revoke emits the
	// same published terminal credential outcome the Client must handle after
	// that cleanup decision; no provider database or private state is read.
	n.Crash()
	if err := s.Revoke(id); err != nil {
		t.Fatal(err)
	}
	n.Start()
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return nativeEnrollmentAbsent(v) && !v.Ephemeral
	})
	if applicationProbe(t, probe, "", "tcp", net.JoinHostPort(peerIP.String(), "24001")) {
		t.Fatal("retired ephemeral client still reached the reference peer")
	}
	n.Stop()

	replacement := testclient.New(t, s)
	replacement.Enroll(s, network.Name, token, "--hostname", "next-ephemeral-job")
	replacement.Start()
	replacementStatus := replacement.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.Ephemeral && v.GetStoredState().GetCachedMapValid() && v.GetStoredState().GetNodeCredentialPresent()
	})
	if replacementStatus.NodeId == id {
		t.Fatal("fresh ephemeral job reused the retired node identity")
	}
}
