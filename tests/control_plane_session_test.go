package tests

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-014: expiry of a user session denies user RPCs without revoking the
// independently enrolled node. Reauthentication restores user RPCs and the
// same node identity and native traffic survive an agent restart.
func TestControlPlaneSessionExpiryRecovery(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, _, err := s.AddNetwork("session-recovery", "198.18.90.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	login := func(token string) {
		t.Helper()
		n.MustRun("login", "--config", n.Config, "--server", s.URL(), "--token", token, "--map-signing-trust-file", n.TrustFile)
	}
	accounts := func(want bool) {
		t.Helper()
		output, err := n.Run("billing", "accounts", "--config", n.Config)
		if want {
			if err != nil || !strings.Contains(string(output), "test-account") {
				t.Fatal("authenticated user RPC did not return the published account")
			}
			return
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatal("expired user session did not fail the user RPC")
		}
	}
	login(s.SessionToken())
	accounts(true)
	n.MustRun("up", "--config", n.Config, "--network", network.Name, "--hostname", "session-node", "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.CachedMapValid && v.NodeCredentialPresent
	})
	nodeID := status.NodeID
	snapshot, err := s.Snapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("198.18.90.20")
	underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, netip.MustParseAddr(status.OverlayIP), peerIP, underlay)
	peer := api.Peer{
		ID: "session-peer", Hostname: "session-peer", PublicKey: reference.PublicKey,
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
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
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
			t.Fatal("node traffic did not remain reachable")
		}
	}
	apply()
	reachable()
	newSession := s.RotateSession()
	accounts(false)
	apply()
	reachable()
	current, err := n.Status()
	if err != nil || current.NodeID != nodeID || !current.NodeCredentialPresent || !current.CachedMapValid {
		t.Fatal("user session expiry changed the independent node enrollment")
	}
	login(newSession)
	accounts(true)
	current, err = n.Status()
	if err != nil || current.NodeID != nodeID || !current.NodeCredentialPresent {
		t.Fatal("reauthentication replaced the enrolled node")
	}
	n.Stop()
	n.Start()
	apply()
	reachable()
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("session recovery created another node registration")
	}
}
