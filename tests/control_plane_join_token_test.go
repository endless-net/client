package tests

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-015: rotating a join credential denies new use of the old token without
// affecting a node credential already issued from it. A replacement token
// restores enrollment for a distinct client.
func TestControlPlaneJoinTokenRotation(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("join-token-rotation", "198.18.89.0/24")
	if err != nil {
		t.Fatal(err)
	}
	existing := testclient.New(t, s)
	existing.Enroll(s, network.Name, token, "--route-table", "auto")
	existing.Start()
	defer existing.Stop()
	status := existing.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.CachedMapValid && v.NodeCredentialPresent
	})
	nodeID := status.NodeID
	snapshot, err := s.Snapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("198.18.89.20")
	underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, netip.MustParseAddr(status.OverlayIP), peerIP, underlay)
	peer := api.Peer{
		ID: "join-rotation-peer", Hostname: "join-rotation-peer", PublicKey: reference.PublicKey,
		Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint},
		AllowedIPs: []string{peerIP.String() + "/32"},
	}
	apply := func() {
		t.Helper()
		if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) { m.Peers = []api.Peer{peer} }); err != nil {
			t.Fatal(err)
		}
		current, err := s.Snapshot(nodeID)
		if err != nil {
			t.Fatal(err)
		}
		status = existing.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == nodeID && v.MapRevision >= current.Revision.Network &&
				v.PeerCount == 1 && v.NodeCredentialPresent && v.CachedMapValid &&
				v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
				v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
				v.WireGuard != nil && v.WireGuard.OK
		})
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
	}
	binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	address := net.JoinHostPort(peerIP.String(), "24001")
	reachable := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool { return applicationProbe(t, binary, "", "tcp", address) }); err != nil {
			t.Fatal("existing node traffic did not remain reachable")
		}
	}
	apply()
	reachable()
	replacement, err := s.RotateJoinToken(token)
	if err != nil {
		t.Fatal(err)
	}
	candidate := testclient.New(t, s)
	_, err = candidate.Run("up", "--config", candidate.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "replacement-node", "--map-signing-trust-file", candidate.TrustFile, "--route-table", "off")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("rotated join token still enrolled a new client")
	}
	apply()
	reachable()
	if current, statusErr := existing.Status(); statusErr != nil || current.NodeID != nodeID || !current.NodeCredentialPresent {
		t.Fatal("join-token rotation changed the existing node credential")
	}
	existing.Stop()
	existing.Start()
	apply()
	reachable()
	replacementClient := testclient.New(t, s)
	replacementClient.Enroll(s, network.Name, replacement, "--hostname", "replacement-node")
	registrations := 0
	registeredIDs := map[string]bool{}
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
			registeredIDs[event.NodeID] = true
		}
	}
	if registrations != 2 || len(registeredIDs) != 2 || !registeredIDs[nodeID] {
		t.Fatal("replacement join token did not enroll exactly one distinct client")
	}
}
