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
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) { exerciseSessionExpiryRecovery(t, family) })
	}
}

func exerciseSessionExpiryRecovery(t *testing.T, family string) {
	t.Helper()
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
	accounts := func(phase string, want bool) {
		t.Helper()
		before := len(s.Events())
		started := time.Now()
		output, err := n.Run("billing", "accounts", "--config", n.Config)
		accepted, denied := 0, 0
		for _, event := range s.Events()[before:] {
			switch event.Kind {
			case "user-accounts-accepted":
				accepted++
			case "user-accounts-denied":
				denied++
			}
		}
		var exit *exec.ExitError
		exitCode := 0
		if err != nil {
			exitCode = -1
			if errors.As(err, &exit) {
				exitCode = exit.ExitCode()
			}
		}
		hasAccount := strings.Contains(string(output), "test-account")
		if want {
			if err != nil || !hasAccount || accepted == 0 || denied != 0 {
				t.Fatalf("authenticated user RPC did not return the published account: phase=%s exit_code=%d output_bytes=%d account_present=%t accepted=%d denied=%d elapsed=%s", phase, exitCode, len(output), hasAccount, accepted, denied, time.Since(started).Round(time.Millisecond))
			}
			return
		}
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || denied == 0 || accepted != 0 {
			t.Fatalf("expired user session did not produce an authorization denial: phase=%s exit_code=%d accepted=%d denied=%d", phase, exitCode, accepted, denied)
		}
	}
	login(s.SessionToken())
	accounts("initial-login", true)
	n.MustRun("up", "--config", n.Config, "--network", network.Name, "--hostname", "session-node", "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.CachedMapValid && v.NodeCredentialPresent
	})
	nodeID := status.NodeID
	clientIP, peerIP := netip.MustParseAddr(status.OverlayIP), netip.MustParseAddr("198.18.90.20")
	if family == "ipv6" {
		if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR, m.Node.AssignedIPv6 = "fd90::/64", "fd90::1"
		}); err != nil {
			t.Fatal(err)
		}
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == nodeID && v.OverlayIPv6 == "fd90::1" && v.CachedMapValid
		})
		clientIP, peerIP = netip.MustParseAddr(status.OverlayIPv6), netip.MustParseAddr("fd90::20")
	}
	snapshot, err := s.Snapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{
		ID: "session-peer", Hostname: "session-peer", PublicKey: reference.PublicKey,
		Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint},
		AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()},
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
		for _, protocol := range []string{"tcp", "udp"} {
			if err := testclient.Await(ctx, func() bool {
				beforeReceived, beforeEchoed := reference.PacketCounts()
				if !applicationProbe(t, binary, "", protocol, address) {
					return false
				}
				received, echoed := reference.PacketCounts()
				return received > beforeReceived && echoed > beforeEchoed
			}); err != nil {
				t.Fatalf("%s lifecycle traffic did not produce a fresh reference peer echo", protocol)
			}
		}
	}
	apply()
	reachable()
	newSession := s.RotateSession()
	accounts("expired-session", false)
	apply()
	reachable()
	current, err := n.Status()
	if err != nil || current.NodeID != nodeID || !current.NodeCredentialPresent || !current.CachedMapValid {
		t.Fatal("user session expiry changed the independent node enrollment")
	}
	// A fresh process must still use the independent node credential while
	// user RPCs remain unauthorized, before any reauthentication can mask it.
	n.Stop()
	n.Start()
	apply()
	reachable()
	accounts("expired-session-after-restart", false)
	login(newSession)
	accounts("reauthenticated", true)
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
