package tests

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// namespaceCommand is fixture setup only. Client state and keys are never read.
func namespaceCommand(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "ip", args...).Run(); err != nil {
		t.Fatalf("namespace setup %s: %v", args[0], err)
	}
}

func peerUnderlay(t *testing.T) [2]string {
	t.Helper()
	prefix := "en" + rand.Text()[:6]
	bridge := prefix + "br"
	namespaceCommand(t, "link", "add", bridge, "type", "bridge")
	t.Cleanup(func() { namespaceCommand(t, "link", "del", bridge) })
	namespaceCommand(t, "addr", "add", "192.0.2.1/24", "dev", bridge)
	namespaceCommand(t, "link", "set", bridge, "up")
	var namespaces [2]string
	for i, host := range []string{"192.0.2.2/24", "192.0.2.3/24"} {
		suffix := []string{"a", "b"}[i]
		ns, outer, inner := prefix+suffix, prefix+suffix+"o", prefix+suffix+"i"
		namespaceCommand(t, "netns", "add", ns)
		t.Cleanup(func() { namespaceCommand(t, "netns", "del", ns) })
		namespaceCommand(t, "link", "add", outer, "type", "veth", "peer", "name", inner)
		namespaceCommand(t, "link", "set", inner, "netns", ns)
		namespaceCommand(t, "link", "set", outer, "master", bridge)
		namespaceCommand(t, "link", "set", outer, "up")
		namespaceCommand(t, "-n", ns, "link", "set", "lo", "up")
		namespaceCommand(t, "-n", ns, "addr", "add", host, "dev", inner)
		namespaceCommand(t, "-n", ns, "link", "set", inner, "up")
		namespaces[i] = ns
	}
	return namespaces
}

func pingPeer(namespace, address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "ip", "netns", "exec", namespace, "ping", "-n", "-c", "1", "-W", "1", address).Run()
}

// HC-024/HC-027/HC-028: actual IP traffic between real agents, isolated so the
// host cannot short-circuit a tunnel by treating both overlay IPs as local.
func TestControlPlaneDirectPeerTrafficAndWithdrawal(t *testing.T) {
	requireControlScenario(t)
	for _, tool := range []string{"ip", "ping"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("CI requires %s", tool)
		}
	}
	namespaces := peerUnderlay(t)
	listener, err := net.Listen("tcp", "192.0.2.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := testcontrol.NewWithListener(t, listener)
	network, token, err := s.AddNetwork("direct-peers", "100.93.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	var nodes [2]*testclient.Node
	var states [2]ipc.StatusResponse
	endpoints := [2]string{"192.0.2.2:51820", "192.0.2.3:51820"}
	for i := range nodes {
		n := testclient.New(t, s)
		n.Namespace = namespaces[i]
		n.AgentArgs = []string{"--listen-port", "51820", "--endpoint", endpoints[i]}
		n.Enroll(s, network.Name, token, "--route-table", "auto")
		n.Start()
		states[i] = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
		nodes[i] = n
	}
	var peers [2]api.Peer
	for i := range nodes {
		remote := 1 - i
		m, err := s.Snapshot(states[remote].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		peers[i] = api.Peer{ID: m.Node.ID, Hostname: m.Node.Hostname, PublicKey: m.Node.PublicKey, Endpoint: endpoints[remote], AllowedIPs: []string{m.Node.AssignedIP + "/32"}}
		if err := s.UpdatePeers(states[i].NodeID, []api.Peer{peers[i]}); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range nodes {
		n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK })
	}
	for i := range nodes {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := testclient.Await(ctx, func() bool { return pingPeer(namespaces[i], states[1-i].OverlayIP) == nil })
		cancel()
		if err != nil {
			t.Fatalf("peer %d did not pass overlay traffic", i)
		}
		nodes[i].AwaitStatus(func(v ipc.StatusResponse) bool {
			if v.WireGuard == nil || len(v.WireGuard.Peers) != 1 {
				return false
			}
			p := v.WireGuard.Peers[0]
			return p.LatestHandshakeUnix > 0 && p.TransferRXBytes > 0 && p.TransferTXBytes > 0
		})
	}
	// Withdraw only the receiving side. Successful denial cannot be explained
	// by removing the sender's route or stopping either client process.
	if err := s.UpdatePeers(states[1].NodeID, nil); err != nil {
		t.Fatal(err)
	}
	nodes[1].AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.PeerCount == 0 && v.WireGuard != nil && v.WireGuard.PeerCount == 0
	})
	for range 3 {
		if pingPeer(namespaces[0], states[1].OverlayIP) == nil {
			t.Fatal("withdrawn peer still accepted overlay traffic")
		}
	}
	if err := s.UpdatePeers(states[1].NodeID, []api.Peer{peers[1]}); err != nil {
		t.Fatal(err)
	}
	nodes[1].AwaitStatus(func(v ipc.StatusResponse) bool { return v.PeerCount == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := testclient.Await(ctx, func() bool { return pingPeer(namespaces[0], states[1].OverlayIP) == nil }); err != nil {
		t.Fatal("restored peer did not recover traffic")
	}
	exerciseApplicationPolicy(t, s, nodes, states, peers)
}

func applicationProbe(t *testing.T, binary, namespace, protocol, address string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "ip", "netns", "exec", namespace, binary, "--mode", "probe", "--network", protocol, "--address", address).Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 2 {
		return false
	}
	t.Fatalf("application probe failed independently of network access: %v", err)
	return false
}

// HC-024/HC-027/HC-031: protocol and destination-port policy with payload checks.
func exerciseApplicationPolicy(t *testing.T, s *testcontrol.Server, nodes [2]*testclient.Node, states [2]ipc.StatusResponse, peers [2]api.Peer) {
	t.Helper()
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	if !filepath.IsAbs(binary) {
		t.Fatal("ENDLESSNET_PACKET_PROBE must identify the CI application peer")
	}
	for _, port := range []int{24001, 24002} {
		cmd := exec.Command("ip", "netns", "exec", nodes[1].Namespace, binary, "--mode", "serve", "--address", "0.0.0.0:"+strconv.Itoa(port))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := testclient.Await(ctx, func() bool {
			return applicationProbe(t, binary, nodes[1].Namespace, "tcp", "127.0.0.1:"+strconv.Itoa(port))
		})
		cancel()
		if err != nil {
			t.Fatal("application listener did not start")
		}
	}
	check := func(restricted bool) {
		t.Helper()
		for _, protocol := range []string{"tcp", "udp"} {
			for _, port := range []int{24001, 24002} {
				address := net.JoinHostPort(states[1].OverlayIP, strconv.Itoa(port))
				allowed := !restricted || (protocol == "tcp" && port == 24001) || (protocol == "udp" && port == 24002)
				if allowed {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := testclient.Await(ctx, func() bool { return applicationProbe(t, binary, nodes[0].Namespace, protocol, address) })
					cancel()
					if err != nil {
						t.Fatalf("allowed %s port %d did not exchange application data", protocol, port)
					}
				} else {
					for range 3 {
						if !applicationProbe(t, binary, nodes[1].Namespace, protocol, "127.0.0.1:"+strconv.Itoa(port)) {
							t.Fatal("denial cannot be checked against an unavailable application")
						}
						if applicationProbe(t, binary, nodes[0].Namespace, protocol, address) {
							t.Fatalf("denied %s port %d passed application data", protocol, port)
						}
					}
				}
			}
		}
	}
	check(false)
	limited := peers[0]
	limited.ACLRestricted = true
	limited.ACLGrants = []api.ACLGrant{{DestinationCIDRs: limited.AllowedIPs, AllowedPorts: []api.ACLPort{{Protocol: "tcp", Port: 24001}, {Protocol: "udp", Port: 24002}}}}
	apply := func(peer api.Peer) {
		t.Helper()
		if err := s.UpdatePeers(states[0].NodeID, []api.Peer{peer}); err != nil {
			t.Fatal(err)
		}
		m, err := s.Snapshot(states[0].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		nodes[0].AwaitStatus(func(v ipc.StatusResponse) bool { return v.MapRevision >= m.Revision.Network && v.PeerCount == 1 })
	}
	apply(limited)
	check(true)
	apply(peers[0])
	check(false)
}
