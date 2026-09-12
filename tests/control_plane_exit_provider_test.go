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
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-038: one real Client advertises and serves as an IPv4 exit provider for
// another real Client. Linux proves forwarding and SNAT with an external
// namespace; other native platforms publish the explicit unsupported outcome.
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
	var states [2]ipc.StatusResponse
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
		states[i] = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID != "" && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK
		})
		nodes[i] = n
	}
	providerMap, err := s.Snapshot(states[1].NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerMap.Node.AdvertisedIPs) != 1 || providerMap.Node.AdvertisedIPs[0] != "0.0.0.0/0" {
		t.Fatal("provider registration lost its advertised default route")
	}
	peerFor := func(remote int, allowed ...string) api.Peer {
		m, err := s.Snapshot(states[remote].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		return api.Peer{ID: m.Node.ID, Hostname: m.Node.Hostname, PublicKey: m.Node.PublicKey, Endpoint: endpoints[remote], EndpointCandidates: []string{endpoints[remote]}, AllowedIPs: allowed}
	}
	providerPeer := peerFor(1, states[1].OverlayIP+"/32")
	sourcePeer := peerFor(0, states[0].OverlayIP+"/32")
	if err := s.UpdatePeers(states[0].NodeID, []api.Peer{providerPeer}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePeers(states[1].NodeID, []api.Peer{sourcePeer}); err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.LastError == ""
		})
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
		if probe("tcp") || probe("udp") {
			t.Fatal("external target was reachable without an approved default route")
		}
	}
	reachable := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool { return probe("tcp") && probe("udp") }); err != nil {
			t.Fatal("approved default route did not pass TCP and UDP through the Client exit provider")
		}
	}
	apply := func(approved bool) {
		t.Helper()
		peer := providerPeer
		if approved {
			peer.AllowedIPs = append(peer.AllowedIPs, "0.0.0.0/0")
		}
		if err := s.UpdatePeers(states[0].NodeID, []api.Peer{peer}); err != nil {
			t.Fatal(err)
		}
		m, err := s.Snapshot(states[0].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		nodes[0].AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.MapRevision >= m.Revision.Network && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == ""
		})
	}

	blocked()
	apply(true)
	reachable()
	apply(false)
	blocked()
	apply(true)
	reachable()
	nodes[1].Stop()
	blocked()
	nodes[1].Start()
	nodes[1].AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == states[1].NodeID && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.LastError == ""
	})
	reachable()
}
