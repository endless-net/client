package tests

import (
	"context"
	"crypto/rand"
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

// HC-034: advertising a subnet, approval of that route, and usable forwarding
// are separate observable results. Linux exercises the real router dataplane;
// other native clients must report that the Linux-only SNAT mode is unsupported.
func TestControlPlaneSubnetRouter(t *testing.T) {
	requireControlScenario(t)
	if runtime.GOOS != "linux" {
		testUnsupportedSubnetRouter(t)
		return
	}
	t.Run("snat", func(t *testing.T) { testLinuxSubnetRouter(t, true) })
	t.Run("preserve-source", func(t *testing.T) { testLinuxSubnetRouter(t, false) })
}

func testUnsupportedSubnetRouter(t *testing.T) {
	t.Helper()
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("unsupported-subnet-router", "100.98.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	output, err := n.Run("up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "subnet-router", "--map-signing-trust-file", n.TrustFile, "--route-table", "off", "--advertise", "198.18.98.0/24", "--advertise-snat")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "advertise-snat is unsupported on "+runtime.GOOS) {
		t.Fatal("unsupported subnet-router mode did not return its explicit platform outcome")
	}
	for _, event := range s.Events() {
		if event.Kind == "registered" || event.Kind == "registration-request" {
			t.Fatal("unsupported subnet-router mode reached registration")
		}
	}
}

func testLinuxSubnetRouter(t *testing.T, snat bool) {
	t.Helper()
	for _, tool := range []string{"ip", "iptables"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("CI requires %s", tool)
		}
	}
	namespaces := peerUnderlay(t)
	resourceNamespace, routerLink := attachRouterResource(t, namespaces[1])
	listener, err := net.Listen("tcp", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := testcontrol.NewWithListener(t, listener)
	network, token, err := s.AddNetwork("subnet-router", "100.98.0.0/24")
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
			options = append(options, "--hostname", "subnet-router", "--advertise", "198.18.98.0/24")
			if snat {
				options = append(options, "--advertise-snat")
			}
		}
		n.Enroll(s, network.Name, token, options...)
		n.Start()
		states[i] = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID != "" && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK
		})
		nodes[i] = n
	}
	if !snat {
		// In routed mode, forwarding and the LAN return route are operator
		// prerequisites. The Client owns the tunnel and signed peer projection.
		namespaceCommand(t, "netns", "exec", namespaces[1], "sysctl", "-w", "net.ipv4.ip_forward=1")
		namespaceCommand(t, "-n", resourceNamespace, "route", "add", states[0].OverlayIP+"/32", "via", "198.18.98.1")
		// Enforce source preservation at the external resource. A translated
		// request cannot satisfy this test even if its reply would be routable.
		namespaceCommand(t, "netns", "exec", resourceNamespace, "iptables", "-A", "INPUT", "-i", "lo", "-j", "ACCEPT")
		namespaceCommand(t, "netns", "exec", resourceNamespace, "iptables", "-A", "INPUT", "-s", states[0].OverlayIP+"/32", "-j", "ACCEPT")
		namespaceCommand(t, "netns", "exec", resourceNamespace, "iptables", "-A", "INPUT", "-j", "DROP")
	}
	routerMap, err := s.Snapshot(states[1].NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(routerMap.Node.AdvertisedIPs) != 1 || routerMap.Node.AdvertisedIPs[0] != "198.18.98.0/24" {
		t.Fatal("router registration lost its advertised subnet")
	}

	peerFor := func(remote int, allowed ...string) api.Peer {
		m, err := s.Snapshot(states[remote].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		return api.Peer{ID: m.Node.ID, Hostname: m.Node.Hostname, PublicKey: m.Node.PublicKey, Endpoint: endpoints[remote], EndpointCandidates: []string{endpoints[remote]}, AllowedIPs: allowed}
	}
	sourceRoute := peerFor(1, states[1].OverlayIP+"/32")
	routerSource := peerFor(0, states[0].OverlayIP+"/32")
	if err := s.UpdatePeers(states[0].NodeID, []api.Peer{sourceRoute}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePeers(states[1].NodeID, []api.Peer{routerSource}); err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.LastError == ""
		})
	}

	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	server := exec.Command("ip", "netns", "exec", resourceNamespace, binary, "--mode", "serve", "--address", "0.0.0.0:24001")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = testclient.Await(ctx, func() bool {
		return applicationProbe(t, binary, resourceNamespace, "tcp", "127.0.0.1:24001") && applicationProbe(t, binary, resourceNamespace, "udp", "127.0.0.1:24001")
	})
	cancel()
	if err != nil {
		t.Fatal("LAN application did not start")
	}
	address := "198.18.98.20:24001"
	probe := func(protocol string) bool { return applicationProbe(t, binary, namespaces[0], protocol, address) }
	assertBlocked := func() {
		t.Helper()
		for range 3 {
			if probe("tcp") || probe("udp") {
				t.Fatal("unapproved or withdrawn subnet route passed application traffic")
			}
		}
	}
	assertReachable := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool { return probe("tcp") && probe("udp") }); err != nil {
			sourceStatus, sourceStatusErr := nodes[0].Status()
			routerStatus, routerStatusErr := nodes[1].Status()
			sourceRX, sourceTX, routerRX, routerTX := uint64(0), uint64(0), uint64(0), uint64(0)
			if sourceStatus.WireGuard != nil {
				for _, peer := range sourceStatus.WireGuard.Peers {
					sourceRX += peer.TransferRXBytes
					sourceTX += peer.TransferTXBytes
				}
			}
			if routerStatus.WireGuard != nil {
				for _, peer := range routerStatus.WireGuard.Peers {
					routerRX += peer.TransferRXBytes
					routerTX += peer.TransferTXBytes
				}
			}
			commandOutput := func(args ...string) ([]byte, bool) {
				commandCtx, commandCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer commandCancel()
				out, commandErr := exec.CommandContext(commandCtx, "ip", args...).Output()
				return out, commandErr == nil
			}
			commandOK := func(args ...string) bool { _, ok := commandOutput(args...); return ok }
			forwardingOutput, forwardingOK := commandOutput("netns", "exec", namespaces[1], "sysctl", "-n", "net.ipv4.ip_forward")
			forwarding := forwardingOK && strings.TrimSpace(string(forwardingOutput)) == "1"
			forwardRule := commandOK("netns", "exec", namespaces[1], "iptables", "-C", "FORWARD", "-i", nodes[1].Interface, "-o", routerLink, "-s", network.CIDR, "-d", "198.18.98.0/24", "-j", "ACCEPT")
			returnRule := commandOK("netns", "exec", namespaces[1], "iptables", "-C", "FORWARD", "-i", routerLink, "-o", nodes[1].Interface, "-s", "198.18.98.0/24", "-d", network.CIDR, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
			natRule := commandOK("netns", "exec", namespaces[1], "iptables", "-t", "nat", "-C", "POSTROUTING", "-s", network.CIDR, "-d", "198.18.98.0/24", "-o", routerLink, "-j", "MASQUERADE")
			t.Logf("subnet router failure: source_status=%t router_status=%t source_route=%t router_route=%t forwarding=%t forward_rule=%t return_rule=%t nat_rule=%t source_rx=%d source_tx=%d router_rx=%d router_tx=%d", sourceStatusErr == nil, routerStatusErr == nil, commandOK("-n", namespaces[0], "route", "get", "198.18.98.20"), commandOK("-n", namespaces[1], "route", "get", "198.18.98.20"), forwarding, forwardRule, returnRule, natRule, sourceRX, sourceTX, routerRX, routerTX)
			t.Fatal("approved subnet route did not pass TCP and UDP through the Client router")
		}
	}
	applySourceRoute := func(peer api.Peer) {
		t.Helper()
		if err := s.UpdatePeers(states[0].NodeID, []api.Peer{peer}); err != nil {
			t.Fatal(err)
		}
		m, err := s.Snapshot(states[0].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		nodes[0].AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.MapRevision >= m.Revision.Network && v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == ""
		})
	}

	assertBlocked()
	approved := sourceRoute
	approved.AllowedIPs = append(approved.AllowedIPs, "198.18.98.0/24")
	applySourceRoute(approved)
	assertReachable()
	if !snat {
		namespaceCommand(t, "-n", resourceNamespace, "route", "del", states[0].OverlayIP+"/32", "via", "198.18.98.1")
		assertBlocked()
		namespaceCommand(t, "-n", resourceNamespace, "route", "add", states[0].OverlayIP+"/32", "via", "198.18.98.1")
		assertReachable()
	}
	applySourceRoute(sourceRoute)
	assertBlocked()
	applySourceRoute(approved)
	assertReachable()

	nodes[1].Stop()
	assertBlocked()
	nodes[1].Start()
	nodes[1].AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == states[1].NodeID && v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.Agent != nil && v.Agent.LastError == ""
	})
	assertReachable()
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 2 {
		t.Fatal("router recovery unexpectedly registered another Client")
	}
}

func attachRouterResource(t *testing.T, routerNamespace string) (string, string) {
	t.Helper()
	prefix := "enl" + strings.ToLower(rand.Text()[:6])
	resourceNamespace := prefix + "r"
	routerLink, resourceLink := prefix+"a", prefix+"b"
	namespaceCommand(t, "netns", "add", resourceNamespace)
	t.Cleanup(func() { namespaceCommand(t, "netns", "del", resourceNamespace) })
	namespaceCommand(t, "link", "add", routerLink, "type", "veth", "peer", "name", resourceLink)
	namespaceCommand(t, "link", "set", routerLink, "netns", routerNamespace)
	namespaceCommand(t, "link", "set", resourceLink, "netns", resourceNamespace)
	namespaceCommand(t, "-n", routerNamespace, "addr", "add", "198.18.98.1/24", "dev", routerLink)
	namespaceCommand(t, "-n", routerNamespace, "link", "set", routerLink, "up")
	namespaceCommand(t, "-n", resourceNamespace, "link", "set", "lo", "up")
	namespaceCommand(t, "-n", resourceNamespace, "addr", "add", "198.18.98.20/24", "dev", resourceLink)
	namespaceCommand(t, "-n", resourceNamespace, "link", "set", resourceLink, "up")
	return resourceNamespace, routerLink
}
