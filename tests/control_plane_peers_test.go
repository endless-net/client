package tests

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
func TestClientDataplaneDirectPeerTrafficAndWithdrawal(t *testing.T) {
	requireControlScenario(t)
	if runtime.GOOS != "linux" {
		t.Fatal("this dataplane fixture requires Linux namespaces")
	}
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
	exerciseConnectionIntent(t, s, nodes, states)
	exercisePeerDNS(t, s, nodes, states, peers)
	exerciseCredentialRetirement(t, s, nodes, states)
}

// HC-017/HC-018/HC-030: persisted user intent and outage behavior must agree with
// application traffic, without a new enrollment or private-state inspection.
func exerciseConnectionIntent(t *testing.T, s *testcontrol.Server, nodes [2]*testclient.Node, initial [2]ipc.StatusResponse) {
	t.Helper()
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	source := nodes[0]
	assertDisconnected := func() {
		t.Helper()
		source.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.State == ipc.StateDisconnected && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected && v.NodeID == initial[0].NodeID && v.NodeCredentialPresent && v.CachedMapValid
		})
		for range 3 {
			if !applicationProbe(t, binary, nodes[1].Namespace, "tcp", "127.0.0.1:24001") {
				t.Fatal("disconnect denial coincided with application failure")
			}
			if applicationProbe(t, binary, source.Namespace, "tcp", net.JoinHostPort(initial[1].OverlayIP, "24001")) {
				t.Fatal("user-disconnected client still passed application traffic")
			}
			if pingPeer(nodes[1].Namespace, initial[0].OverlayIP) == nil {
				t.Fatal("user-disconnected client still received overlay traffic")
			}
		}
	}
	assertTraffic := func() {
		t.Helper()
		source.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == initial[0].NodeID && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected && v.WireGuard != nil && v.WireGuard.OK
		})
		for i := range nodes {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := testclient.Await(ctx, func() bool { return pingPeer(nodes[i].Namespace, initial[1-i].OverlayIP) == nil })
			cancel()
			if err != nil {
				t.Fatal("connection intent did not restore bidirectional traffic")
			}
		}
		for _, protocol := range []string{"tcp", "udp"} {
			if !applicationProbe(t, binary, source.Namespace, protocol, net.JoinHostPort(initial[1].OverlayIP, "24001")) {
				t.Fatalf("connected client did not pass %s application traffic", protocol)
			}
		}
	}
	var disconnected ipc.DisconnectResponse
	source.Service("disconnect", &disconnected)
	assertDisconnected()
	source.Stop()
	source.Start()
	assertDisconnected()
	var connected ipc.ConnectResponse
	source.Service("connect", &connected)
	assertTraffic()
	// A repeated connect and a process restart must preserve connected intent.
	source.Service("connect", &connected)
	source.Stop()
	source.Start()
	assertTraffic()
	s.SetUnavailable(true)
	for _, n := range nodes {
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.State == ipc.StateDegraded && v.NodeCredentialPresent && v.CachedMapValid
		})
	}
	assertTraffic()
	s.SetUnavailable(false)
	update(t, s, initial[0].NodeID, func(m *api.NetworkMapSnapshot) { m.Network.Name = "control-restored" })
	recovered, err := s.Snapshot(initial[0].NodeID)
	if err != nil {
		t.Fatal(err)
	}
	source.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == initial[0].NodeID && v.MapRevision >= recovered.Revision.Network && v.State == ipc.StateConnected
	})
	assertTraffic()
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
		if event.Kind == "registration-refreshed" && event.NodeID != initial[0].NodeID && event.NodeID != initial[1].NodeID {
			t.Fatal("credential refresh changed node identity")
		}
	}
	if registrations != 2 {
		t.Fatal("local intent or temporary outage caused another enrollment")
	}
}

// HC-025: public DNS CLI/proxy and real applications using that resolver. Each
// dns serve invocation intentionally loads a fresh signed-map snapshot; this
// does not claim OS resolver integration or a live reload contract for the CLI.
func exercisePeerDNS(t *testing.T, s *testcontrol.Server, nodes [2]*testclient.Node, states [2]ipc.StatusResponse, peers [2]api.Peer) {
	t.Helper()
	n := nodes[0]
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	const domain = "scenario.endlessnet"
	const dnsAddress = "127.0.0.1:1053"
	peer := peers[0]
	peer.Hostname = "dns-peer"
	name := peer.Hostname + "." + domain
	lookup := func(query, expected string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ip", "netns", "exec", n.Namespace, binary, "--mode", "resolve", "--dns", dnsAddress, "--address", query).Output()
		if expected == "" {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 {
				t.Fatal("absent peer did not return DNS name-not-found")
			}
		} else if err != nil || strings.TrimSpace(string(out)) != expected {
			t.Fatal("peer DNS returned an unexpected address")
		}
	}
	for _, present := range []bool{true, false, true} {
		var desired []api.Peer
		if present {
			desired = []api.Peer{peer}
		}
		if err := s.UpdatePeers(states[0].NodeID, desired); err != nil {
			t.Fatal(err)
		}
		m, err := s.Snapshot(states[0].NodeID)
		if err != nil {
			t.Fatal(err)
		}
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.MapRevision >= m.Revision.Network && v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent && v.Agent.LastError == "" && v.PeerCount == len(desired)
		})
		cmd := exec.Command("ip", "netns", "exec", n.Namespace, n.Binary, "dns", "serve", "--config", n.Config, "--listen", dnsAddress, "--domain", domain)
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
		t.Cleanup(stop)
		ready := make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(output)
			ready <- scanner.Scan() && strings.HasPrefix(scanner.Text(), "dns proxy listening on ")
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("public DNS proxy did not start")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("public DNS proxy startup deadline")
		}
		lookup(states[0].Hostname+"."+domain, states[0].OverlayIP)
		if present {
			lookup(name, states[1].OverlayIP)
			resolved := n.MustRun("dns", "resolve", "--config", n.Config, "--domain", domain, "--name", name)
			if strings.TrimSpace(string(resolved)) != states[1].OverlayIP {
				t.Fatal("DNS CLI differs from DNS wire resolution")
			}
			for _, protocol := range []string{"tcp", "udp"} {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := testclient.Await(ctx, func() bool {
					return applicationProbe(t, binary, n.Namespace, protocol, net.JoinHostPort(name, "24001"), "--dns", dnsAddress)
				})
				cancel()
				if err != nil {
					t.Fatal("application could not access peer by DNS name")
				}
			}
		} else {
			lookup(name, "")
		}
		lookup("absent."+domain, "")
		stop()
	}
}

func applicationProbe(t *testing.T, binary, namespace, protocol, address string, options ...string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []string{"--mode", "probe", "--network", protocol, "--address", address}
	err := packetProbeCommand(ctx, namespace, binary, append(args, options...)...).Run()
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

func startApplicationSession(t *testing.T, binary, namespace, protocol, address string) func(string) {
	t.Helper()
	cmd := packetProbeCommand(context.Background(), namespace, binary, "--mode", "session", "--network", protocol, "--address", address)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case got, ok := <-lines:
			if !ok || got != want {
				observed := "invalid output"
				if !ok {
					observed = "closed"
				} else if got == "ok" || got == "blocked" || got == "ready" {
					observed = got
				}
				t.Fatalf("persistent %s application session reported %s, expected %s", protocol, observed, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("persistent %s application session did not respond", protocol)
		}
	}
	expect("ready")
	return func(want string) {
		t.Helper()
		if _, err := fmt.Fprintln(input, "exchange"); err != nil {
			t.Fatal("cannot command persistent application session")
		}
		expect(want)
	}
}

func packetProbeCommand(ctx context.Context, namespace, binary string, args ...string) *exec.Cmd {
	if namespace != "" {
		return exec.CommandContext(ctx, "ip", append([]string{"netns", "exec", namespace, binary}, args...)...)
	}
	return exec.CommandContext(ctx, binary, args...)
}

// HC-024/HC-027: published protocol and destination-port policy with payload checks.
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
		nodes[0].AwaitStatus(func(v ipc.StatusResponse) bool {
			// Cached metadata may precede actual tunnel/ACL application. Require
			// the public successful agent snapshot for that same current map.
			return v.MapRevision >= m.Revision.Network && v.PeerCount == 1 && v.Agent != nil && v.Agent.StatePresent && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" && v.WireGuard != nil && v.WireGuard.OK
		})
	}
	apply(limited)
	check(true)
	// Keep the same source sockets open across withdrawal. Fresh connection
	// failures alone do not prove that original-direction established flows stop.
	var sessions []func(string)
	for _, flow := range []struct {
		protocol string
		port     int
	}{{"tcp", 24001}, {"udp", 24002}} {
		session := startApplicationSession(t, binary, nodes[0].Namespace, flow.protocol, net.JoinHostPort(states[1].OverlayIP, strconv.Itoa(flow.port)))
		session("ok")
		sessions = append(sessions, session)
	}
	// A narrower replacement must revoke TCP without interrupting the UDP
	// grant on the same peer. Observe both old sockets and fresh traffic.
	limited.ACLGrants = []api.ACLGrant{{DestinationCIDRs: limited.AllowedIPs, AllowedPorts: []api.ACLPort{{Protocol: "udp", Port: 24002}}}}
	apply(limited)
	for range 3 {
		sessions[1]("ok")
		sessions[0]("blocked")
		if !applicationProbe(t, binary, nodes[0].Namespace, "udp", net.JoinHostPort(states[1].OverlayIP, "24002")) {
			t.Fatal("TCP withdrawal interrupted another authorized overlay flow")
		}
		if !applicationProbe(t, binary, nodes[1].Namespace, "tcp", "127.0.0.1:24001") {
			t.Fatal("TCP denial coincided with application failure")
		}
		if applicationProbe(t, binary, nodes[0].Namespace, "tcp", net.JoinHostPort(states[1].OverlayIP, "24001")) {
			t.Fatal("withdrawn TCP grant still passed fresh traffic")
		}
		sessions[1]("ok")
	}
	limited.ACLGrants = nil
	apply(limited)
	for _, session := range sessions {
		session("blocked")
	}
	for _, flow := range []struct {
		protocol string
		port     int
	}{{"tcp", 24001}, {"udp", 24002}} {
		if !applicationProbe(t, binary, nodes[1].Namespace, flow.protocol, "127.0.0.1:"+strconv.Itoa(flow.port)) {
			t.Fatal("established-flow denial coincided with application failure")
		}
	}
	apply(peers[0])
	check(false)
}
