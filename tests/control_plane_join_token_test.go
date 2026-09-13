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
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
)

// HC-015: rotating a join credential denies new use of the old token without
// affecting a node credential already issued from it. A replacement token
// restores enrollment for a distinct client.
func TestControlPlaneJoinTokenRotation(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) { exerciseJoinTokenRetirement(t, family, false) })
	}
}

func TestControlPlaneJoinTokenExpiryRecovery(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) { exerciseJoinTokenRetirement(t, family, true) })
	}
}

func exerciseJoinTokenRetirement(t *testing.T, family string, expire bool) {
	t.Helper()
	s := testcontrol.NewTLS(t)
	network, token, err := s.AddNetwork("join-token-rotation", "198.18.89.0/24")
	if err != nil {
		t.Fatal(err)
	}
	existing := testclient.New(t, s)
	existing.TrustControlTLS(s)
	existing.Enroll(s, network.Name, token, "--route-table", "auto")
	existing.Start()
	defer existing.Stop()
	status := existing.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId != "" && v.GetStoredState().GetCachedMapValid() && v.GetStoredState().GetNodeCredentialPresent() && nativeOverlayAddress(v, false).IsValid()
	})
	nodeID := status.NodeId
	clientIP, peerIP := nativeOverlayAddress(status, false), netip.MustParseAddr("198.18.89.20")
	if family == "ipv6" {
		if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR, m.Node.AssignedIPv6 = "fd89::/64", "fd89::1"
		}); err != nil {
			t.Fatal(err)
		}
		status = existing.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == nodeID && nativeOverlayAddress(v, true).String() == "fd89::1" && v.GetStoredState().GetCachedMapValid()
		})
		clientIP, peerIP = nativeOverlayAddress(status, true), netip.MustParseAddr("fd89::20")
	}
	snapshot, err := s.Snapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	underlay := nativePeerUnderlay(t, nativeOverlayAddress(status, false), peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{
		ID: "join-rotation-peer", Hostname: "join-rotation-peer", PublicKey: reference.PublicKey,
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
		status = existing.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == nodeID && v.MapRevision >= current.Revision.Network &&
				v.PeerCount == 1 && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
				v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT &&
				v.Agent.MapRevision == v.MapRevision && v.Agent.LastFailure == nil &&
				v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, existing, status)))
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
	var replacement string
	if expire {
		deadline := time.Now().Add(250 * time.Millisecond)
		if err := s.SetJoinTokenExpiry(token, deadline); err != nil {
			t.Fatal(err)
		}
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-t.Context().Done():
			t.Fatal("join-token expiry observation interrupted")
		}
	} else {
		replacement, err = s.RotateJoinToken(token)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Reuse this test's OS trust installation; New supplies a separate Linux CA
	// file for the candidate process without duplicating OS certificate cleanup.
	candidate := testclient.New(t, s)
	beforeDeniedAttempt := len(s.Events())
	_, err = candidate.Run("up", "--config", candidate.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "replacement-node", "--map-signing-trust-file", candidate.TrustFile, "--route-table", "off")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("retired join token still enrolled a new client")
	}
	wantDenial := "join-token-unknown"
	if expire {
		wantDenial = "join-token-expired"
	}
	denialObserved := false
	for _, event := range s.Events()[beforeDeniedAttempt:] {
		denialObserved = denialObserved || event.Kind == wantDenial
	}
	if !denialObserved {
		t.Fatal("failed CLI attempt did not reach the intended token authorization denial")
	}
	apply()
	reachable()
	current := &ipc.GetStatusResponse{}
	if statusErr := existing.NativeService("status", current); statusErr != nil || current.GetStatus().GetNodeId() != nodeID || !current.GetStatus().GetStoredState().GetNodeCredentialPresent() {
		t.Fatal("join-token retirement changed the existing node credential")
	}
	// The retired join token must not become a startup dependency when the
	// existing node restarts from its still-valid map during a control outage.
	s.SetUnavailable(true)
	defer s.SetUnavailable(false)
	existing.Stop()
	existing.Start()
	status = existing.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == nodeID && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
			!v.UserDisconnected && nativeCurrentAgentFailure(v) &&
			v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
	})
	reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, existing, status)))
	reachable()
	s.SetUnavailable(false)
	apply()
	reachable()
	if expire {
		replacement, err = s.RotateJoinToken(token)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Retry the actual failed Client, retaining its public CLI configuration
	// path. A fresh instance would hide poisoned enrollment/retry state.
	candidate.Enroll(s, network.Name, replacement, "--hostname", "replacement-node")
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
