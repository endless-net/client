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
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
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
	s := testcontrol.NewTLS(t)
	network, _, err := s.AddNetwork("session-recovery", "198.18.90.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.TrustControlTLS(s)
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
				t.Fatalf("authenticated user RPC did not return the published account: phase=%s exit_code=%d failure_stage=%s output_bytes=%d account_present=%t accepted=%d denied=%d elapsed=%s", phase, exitCode, userAccountFailureStage(output), len(output), hasAccount, accepted, denied, time.Since(started).Round(time.Millisecond))
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
	runNativeControlMutation(t, n, "connect", "6b140000-0000-4000-8000-000000000001")
	defer n.Stop()
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid() && v.GetStoredState().GetNodeCredentialPresent() && nativeOverlayAddress(v, false).IsValid()
	})
	nodeID := status.NodeId
	profileID := status.ActiveProfileId
	clientIP, peerIP := nativeOverlayAddress(status, false), netip.MustParseAddr("198.18.90.20")
	if family == "ipv6" {
		if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR, m.Node.AssignedIPv6 = "fd90::/64", "fd90::1"
		}); err != nil {
			t.Fatal(err)
		}
		status = n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == nodeID && nativeOverlayAddress(v, true).String() == "fd90::1" && v.GetStoredState().GetCachedMapValid()
		})
		clientIP, peerIP = nativeOverlayAddress(status, true), netip.MustParseAddr("fd90::20")
	}
	snapshot, err := s.Snapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	underlay := nativePeerUnderlay(t, nativeOverlayAddress(status, false), peerIP)
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
		status = n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == nodeID && v.ActiveProfileId == profileID && v.MapRevision >= current.Revision.Network &&
				v.PeerCount == 1 && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
				v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT &&
				v.Agent.MapRevision == v.MapRevision && v.Agent.LastFailure == nil &&
				v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, n, status)))
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
	current := &ipc.GetStatusResponse{}
	if n.NativeService("status", current) != nil || current.GetStatus().GetNodeId() != nodeID || current.GetStatus().GetActiveProfileId() != profileID || !current.GetStatus().GetStoredState().GetNodeCredentialPresent() || !current.GetStatus().GetStoredState().GetCachedMapValid() {
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
	current = &ipc.GetStatusResponse{}
	if n.NativeService("status", current) != nil || current.GetStatus().GetNodeId() != nodeID || current.GetStatus().GetActiveProfileId() != profileID || !current.GetStatus().GetStoredState().GetNodeCredentialPresent() {
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

// Only fixed classifications leave the subprocess boundary, never state paths,
// server responses, account data or authorization material from raw output.
func userAccountFailureStage(output []byte) string {
	message := strings.ToLower(strings.TrimSpace(string(output)))
	switch {
	case strings.HasPrefix(message, "list accounts:"):
		return "user-rpc"
	case strings.HasPrefix(message, "unprotect client state "):
		return "state-unprotect"
	case strings.Contains(message, "sharing violation"), strings.Contains(message, "being used by another process"), strings.Contains(message, "access is denied"):
		return "file-access"
	case strings.HasPrefix(message, "open "), strings.HasPrefix(message, "createfile "), strings.HasPrefix(message, "lstat "), strings.HasPrefix(message, "getfileattributesex "):
		return "state-file"
	case strings.HasPrefix(message, "not logged in;"):
		return "local-session-absent"
	case strings.HasPrefix(message, "control url"):
		return "control-origin"
	default:
		return "unclassified"
	}
}

func TestUserAccountFailureStageDoesNotExposeOutput(t *testing.T) {
	for _, tc := range []struct{ output, stage string }{
		{"list accounts: private server response", "user-rpc"},
		{"unprotect client state private-path: private detail", "state-unprotect"},
		{"CreateFile private-path: Access is denied.", "file-access"},
		{"open private-path: missing file", "state-file"},
		{"not logged in; private detail", "local-session-absent"},
		{"control URL private value", "control-origin"},
		{"private unexpected output", "unclassified"},
	} {
		if userAccountFailureStage([]byte(tc.output)) != tc.stage {
			t.Fatal("account failure classification lost its fixed boundary")
		}
	}
}
