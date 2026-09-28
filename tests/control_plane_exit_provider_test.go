package tests

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

// HC-036–038 boundaries: a real Client advertises an IPv4 default route and
// forwards/SNATs traffic to an external namespace. Its consumer proves that a
// default advertisement cannot select an exit implicitly, then exercises the
// explicit selection, LAN policy and clear paths. Other native platforms
// publish the explicit unsupported provider outcome.
func TestControlPlaneExitProvider(t *testing.T) {
	requireControlScenario(t)
	if runtime.GOOS != "linux" {
		testUnsupportedExitProvider(t)
		return
	}
	testLinuxExitProvider(t)
}

func testUnsupportedExitProvider(t *testing.T) {
	t.Helper()
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("unsupported-exit-provider", "100.97.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	output, err := n.Run("up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "exit-provider", "--map-signing-trust-file", n.TrustFile, "--route-table", "off", "--advertise", "0.0.0.0/0", "--advertise-snat")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "advertise-snat is unsupported on "+runtime.GOOS) {
		t.Fatal("unsupported exit-provider mode did not return its explicit platform outcome")
	}
	for _, event := range s.Events() {
		if event.Kind == "registered" || event.Kind == "registration-request" {
			t.Fatal("unsupported exit-provider mode reached registration")
		}
	}
}

func testLinuxExitProvider(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ip", "iptables"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("CI requires %s", tool)
		}
	}
	namespaces := peerUnderlay(t)
	externalNamespace, routerLink := attachRouterResource(t, namespaces[1])
	namespaceCommand(t, "-n", namespaces[1], "route", "add", "default", "via", "198.18.98.20", "dev", routerLink)
	namespaceCommand(t, "-n", externalNamespace, "addr", "add", "203.0.113.20/32", "dev", "lo")

	listener, err := net.Listen("tcp", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := testcontrol.NewWithListener(t, listener)
	network, token, err := s.AddNetwork("exit-provider", "100.97.0.0/24")
	if err != nil {
		t.Fatal(err)
	}

	endpoints := [2]string{"192.0.2.2:51820", "192.0.2.3:51820"}
	var nodes [2]*testclient.Node
	var states [2]*ipc.Status
	for i := range nodes {
		n := testclient.New(t, s)
		n.Namespace = namespaces[i]
		n.AgentArgs = []string{"--listen-port", "51820", "--endpoint", endpoints[i]}
		options := []string{"--route-table", "auto"}
		if i == 1 {
			options = append(options, "--hostname", "exit-provider", "--advertise", "0.0.0.0/0", "--advertise-snat")
		}
		n.Enroll(s, network.Name, token, options...)
		n.Start()
		runNativeControlMutation(t, n, "connect", "6b130000-0000-4000-8000-000000000001")
		states[i] = n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid() && nativeOverlayAddress(v, false).IsValid() && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
		nodes[i] = n
	}
	providerMap, err := s.Snapshot(states[1].NodeId)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerMap.Node.AdvertisedIPs) != 1 || providerMap.Node.AdvertisedIPs[0] != "0.0.0.0/0" {
		t.Fatal("provider registration lost its advertised default route")
	}
	peerFor := func(remote int, allowed ...string) api.Peer {
		m, err := s.Snapshot(states[remote].NodeId)
		if err != nil {
			t.Fatal(err)
		}
		return api.Peer{ID: m.Node.ID, Hostname: m.Node.Hostname, PublicKey: m.Node.PublicKey, Endpoint: endpoints[remote], EndpointCandidates: []string{endpoints[remote]}, AllowedIPs: allowed}
	}
	providerPeer := peerFor(1, nativeOverlayAddress(states[1], false).String()+"/32")
	sourcePeer := peerFor(0, nativeOverlayAddress(states[0], false).String()+"/32")
	if err := s.UpdatePeers(states[0].NodeId, []api.Peer{providerPeer}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePeers(states[1].NodeId, []api.Peer{sourcePeer}); err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		current, err := s.Snapshot(states[i].NodeId)
		if err != nil {
			t.Fatal(err)
		}
		states[i] = awaitNativePeerMap(t, n, states[i], current.Revision.Network, 1)
	}

	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	server := exec.Command("ip", "netns", "exec", externalNamespace, binary, "--mode", "serve", "--address", "203.0.113.20:24001")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	err = testclient.Await(ctx, func() bool { return applicationProbe(t, binary, externalNamespace, "tcp", "203.0.113.20:24001") })
	cancel()
	if err != nil {
		t.Fatal("external application did not start")
	}
	probe := func(protocol string) bool {
		return applicationProbe(t, binary, namespaces[0], protocol, "203.0.113.20:24001")
	}
	blocked := func() {
		t.Helper()
		tcpOK, udpOK := probe("tcp"), probe("udp")
		if tcpOK || udpOK {
			t.Fatal("external target was reachable without an authorized resource route or explicit exit selection")
		}
	}
	reachable := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool { return probe("tcp") && probe("udp") }); err != nil {
			t.Fatal("authorized resource route did not pass TCP and UDP through the Client provider")
		}
	}
	apply := func(approved bool) {
		t.Helper()
		peer := providerPeer
		peer.AllowedIPs = append(peer.AllowedIPs, "0.0.0.0/0")
		if approved {
			peer.AllowedIPs = append(peer.AllowedIPs, "203.0.113.20/32")
		}
		if err := s.UpdatePeers(states[0].NodeId, []api.Peer{peer}); err != nil {
			t.Fatal(err)
		}
		m, err := s.Snapshot(states[0].NodeId)
		if err != nil {
			t.Fatal(err)
		}
		states[0] = awaitNativePeerMap(t, nodes[0], states[0], m.Revision.Network, 1)
	}

	blocked()
	apply(false)
	blocked()
	apply(true)
	reachable()
	var sessions []func(string)
	for _, protocol := range []string{"tcp", "udp"} {
		session := startApplicationSession(t, binary, namespaces[0], protocol, "203.0.113.20:24001")
		session("ok")
		sessions = append(sessions, session)
	}
	apply(false)
	for _, session := range sessions {
		session("blocked")
	}
	blocked()
	apply(true)
	reachable()
	nodes[1].Stop()
	blocked()
	nodes[1].Start()
	states[1] = awaitNativePeerMap(t, nodes[1], states[1], states[1].MapRevision, 1)
	reachable()
	// Without an explicit consumer exit selection, ordinary resource routing
	// must keep local LAN access independent from the provider's advertisement.
	{
		lanNamespace, lanLink := attachRouterResource(t, namespaces[0])
		namespaceCommand(t, "-n", lanNamespace, "addr", "add", "10.88.0.20/32", "dev", "lo")
		namespaceCommand(t, "-n", namespaces[0], "route", "add", "10.88.0.20/32", "via", "198.18.98.20", "dev", lanLink)
		lanServer := exec.Command("ip", "netns", "exec", lanNamespace, binary, "--mode", "serve", "--address", "10.88.0.20:24001")
		if err := lanServer.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lanServer.Process.Kill(); _ = lanServer.Wait() })
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		err := testclient.Await(ctx, func() bool {
			return applicationProbe(t, binary, lanNamespace, "tcp", "10.88.0.20:24001") &&
				applicationProbe(t, binary, lanNamespace, "udp", "10.88.0.20:24001")
		})
		cancel()
		if err != nil {
			t.Fatal("local LAN application did not start")
		}
		lanAccess := func(want bool) {
			t.Helper()
			for _, protocol := range []string{"tcp", "udp"} {
				if got := applicationProbe(t, binary, namespaces[0], protocol, "10.88.0.20:24001"); got != want {
					t.Fatalf("exit LAN policy: protocol=%s reachable=%t want=%t", protocol, got, want)
				}
			}
		}
		lanAccess(true)
		reachable()
		apply(false)
		blocked()
		lanAccess(true)
		apply(true)
		lanAccess(true)
		reachable()
		nodes[0].Stop()
		nodes[0].Start()
		states[0] = awaitNativePeerMap(t, nodes[0], states[0], states[0].MapRevision, 1)
		lanAccess(true)
		reachable()

		// A signed grant is required in addition to the provider's advertised
		// default route. The same live consumer then verifies actual forwarding,
		// LAN_BLOCK/LAN_ALLOW effects and durable clear through native IPC.
		apply(false)
		blocked()
		providerIdentity, err := s.Snapshot(states[1].NodeId)
		if err != nil {
			t.Fatal(err)
		}
		grant := api.ExitNodeGrant{
			ID: "contract-exit", Name: "contract exit",
			Host:               api.ServiceHost{NodeID: providerIdentity.Node.ID, PublicKey: providerIdentity.Node.PublicKey},
			AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only},
			AllowedLANAccess:   []api.ExitLANAccess{api.ExitLANBlock, api.ExitLANAllow},
			ExpiresAt:          time.Now().Add(time.Hour),
		}
		if err := s.UpdateMap(states[0].NodeId, func(m *api.NetworkMapSnapshot) {
			if m.Network.ClientPolicy == nil {
				m.Network.ClientPolicy = &api.ClientPolicy{}
			}
			m.Network.ClientPolicy.ExitNodes = []api.ExitNodeGrant{grant}
		}); err != nil {
			t.Fatal(err)
		}
		consumerMap, err := s.Snapshot(states[0].NodeId)
		if err != nil {
			t.Fatal(err)
		}
		states[0] = awaitNativePeerMap(t, nodes[0], states[0], consumerMap.Revision.Network, 1)

		mutateExit := func(requestID, command, lan string) {
			t.Helper()
			current := nodes[0].AwaitNativeStatus(nativeControlSnapshotReady)
			args := testclient.NativeMutationArguments(requestID, current)
			var op *ipc.Operation
			switch command {
			case "select-exit-node":
				args = append(args, "--exit-node-id", grant.ID, "--family-mode", "ipv4-only", "--lan-access", lan)
				response := &ipc.SelectExitNodeResponse{}
				if err := nodes[0].NativeService(command, response, args...); err != nil {
					t.Fatal(err)
				}
				op = response.GetOperation()
			case "clear-exit-node":
				response := &ipc.ClearExitNodeResponse{}
				if err := nodes[0].NativeService(command, response, args...); err != nil {
					t.Fatal(err)
				}
				op = response.GetOperation()
			default:
				t.Fatal("unexpected exit mutation")
			}
			if op == nil || op.GetId() == "" || nodes[0].AwaitNativeOperation(op.GetId()).GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatal("native exit mutation did not succeed")
			}
		}
		awaitExit := func(wantID string, wantLAN ipc.LanAccess) {
			t.Helper()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			var last *ipc.ExitNodeStatus
			var lastErr error
			err := testclient.Await(ctx, func() bool {
				response := &ipc.GetExitNodeResponse{}
				lastErr = nodes[0].NativeService("exit-node", response, "--profile-id", states[0].ActiveProfileId, "--timeout", "1s")
				last = response.GetStatus()
				if lastErr != nil || last == nil {
					return false
				}
				if wantID == "" {
					return last.GetEffectiveExitNodeId() == "" && last.GetApplyState() == ipc.ApplyState_APPLY_STATE_APPLIED
				}
				return last.GetRequestedExitNodeId() == wantID && last.GetEffectiveExitNodeId() == wantID &&
					last.GetRequestedLanAccess() == wantLAN && last.GetEffectiveLanAccess() == wantLAN &&
					last.GetApplyState() == ipc.ApplyState_APPLY_STATE_APPLIED &&
					last.GetIpv4().GetEffectiveExitNodeId() == wantID &&
					last.GetIpv4().GetApplyState() == ipc.ApplyState_APPLY_STATE_APPLIED &&
					!last.GetIpv4().GetFailClosed() && last.GetIpv6().GetEffectiveExitNodeId() == "" &&
					last.GetIpv6().GetApplyState() == ipc.ApplyState_APPLY_STATE_APPLIED &&
					!last.GetIpv6().GetFailClosed() && !last.GetFailClosed()
			})
			if err != nil {
				t.Fatalf("exit runtime did not reach requested public state: requested=%t effective=%t ipv4=%d ipv6=%d fail_closed=%t error=%v",
					last.GetRequestedExitNodeId() == wantID, last.GetEffectiveExitNodeId() == wantID,
					last.GetIpv4().GetApplyState(), last.GetIpv6().GetApplyState(), last.GetFailClosed(), lastErr)
			}
		}

		mutateExit("6b130000-0000-4000-8000-000000000002", "select-exit-node", "block")
		awaitExit(grant.ID, ipc.LanAccess_LAN_ACCESS_BLOCK)
		reachable()
		lanAccess(false)
		mutateExit("6b130000-0000-4000-8000-000000000003", "select-exit-node", "allow")
		awaitExit(grant.ID, ipc.LanAccess_LAN_ACCESS_ALLOW)
		reachable()
		lanAccess(true)
		mutateExit("6b130000-0000-4000-8000-000000000004", "clear-exit-node", "")
		awaitExit("", ipc.LanAccess_LAN_ACCESS_UNSPECIFIED)
		blocked()
		lanAccess(true)
	}
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
			if event.NodeID != states[0].NodeId && event.NodeID != states[1].NodeId {
				t.Fatal("exit-provider recovery replaced a client identity")
			}
		}
	}
	if registrations != 2 {
		t.Fatal("provider or consumer restart created another registration")
	}
}
