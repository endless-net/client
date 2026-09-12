package tests

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
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
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.Ephemeral && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK
	})
	id := status.NodeID
	snapshot, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	clientIP := netip.MustParseAddr(status.OverlayIP)
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
	status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.Ephemeral && v.MapRevision >= current.Revision.Network && v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK
	})
	reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
	probe := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	if err := testclient.Await(ctx, func() bool {
		return applicationProbe(t, probe, "", "tcp", net.JoinHostPort(peerIP.String(), "24001"))
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
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && !v.Ephemeral && !v.NodeCredentialPresent && !v.CachedMapPresent
	})
	n.Stop()

	replacement := testclient.New(t, s)
	replacement.Enroll(s, network.Name, token, "--hostname", "next-ephemeral-job")
	replacement.Start()
	replacementStatus := replacement.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.Ephemeral && v.CachedMapValid
	})
	if replacementStatus.NodeID == id {
		t.Fatal("fresh ephemeral job reused the retired node identity")
	}
}
