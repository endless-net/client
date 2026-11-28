//go:build linux && integration

package clientapp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	flow "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
	signingwire "github.com/endless-net/signing/api/v1"
	"github.com/endless-net/signing/api/v1/signingapiconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const fixtureTunnelPort = 51820

func TestLinuxBootstrapOutageRestoresNativeTunnel(t *testing.T) { runLinuxConnectionFixture(t, nil) }
func TestLinuxFlowConsentReportsNativeEgress(t *testing.T) {
	runLinuxConnectionFixture(t, checkLinuxFlow)
}
func TestLinuxFlowRevocationDropsUnacknowledgedWindow(t *testing.T) {
	runLinuxConnectionFixture(t, checkLinuxPendingRevocation)
}
func TestLinuxFlowExpiryAndRenewalDuringPolicyOutage(t *testing.T) {
	runLinuxConnectionFixture(t, func(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
		monitor := d.native.(*dataplane.LinuxSession).FlowMonitor()
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return local.Applied && monitor.Collecting() })
		// Preserve the current consent identity but shorten the node's reporting window.
		deadline := time.Now().Add(2 * time.Second)
		policy := &flow.GetFlowLogPolicyResponse{ConsentVersion: 3, CollectionNotBefore: timestamppb.New(f.flowStart.Add(-time.Second)), CollectionExpiresAt: timestamppb.New(deadline)}
		f.flowPolicy.Store(policy)
		f.holdWindows.Store(true)
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		send := func(port int) {
			t.Helper()
			if _, err := conn.WriteToUDP([]byte("fixture"), &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: port}); err != nil {
				t.Fatal(err)
			}
		}
		receive := func() *flow.ReportFlowLogRequest {
			t.Helper()
			select {
			case r := <-f.reports:
				return r
			case <-time.After(3 * time.Second):
				t.Fatal("missing native report")
				return nil
			}
		}
		send(443)
		first := receive()
		if first.ConsentVersion != 3 || first.Window.GetDestinationPort() != 443 || !proto.Equal(first, receive()) {
			t.Fatal("expected retry of original window")
		}
		f.policyUnavailable.Store(true)
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return local.Applied && !monitor.Collecting() })
		send(444)
		// Drain requests already observed by the fixture before installing a new grant.
		for len(f.reports) > 0 {
			<-f.reports
		}
		calls := f.reportCalls.Load()
		time.Sleep(2 * d.factory.config.RenewalInterval)
		if calls != f.reportCalls.Load() {
			t.Fatal("expired consent still reported during outage")
		}
		renewed := time.Now()
		f.flowPolicy.Store(&flow.GetFlowLogPolicyResponse{ConsentVersion: 5, CollectionNotBefore: timestamppb.New(renewed), CollectionExpiresAt: timestamppb.New(renewed.Add(time.Minute))})
		f.holdWindows.Store(false)
		f.policyUnavailable.Store(false)
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return local.Applied && monitor.Collecting() })
		send(445)
		next := receive()
		if next.ConsentVersion != 5 || next.Window.GetDestinationPort() != 445 || next.Window.GetPackets() != 1 || next.Window.GetWindowId() == first.Window.GetWindowId() || next.Window.WindowStart.AsTime().Before(renewed) {
			t.Fatal("new consent inherited earlier traffic", next.Window)
		}
		select {
		case extra := <-f.reports:
			t.Fatal("unexpected report after renewal", extra.Window)
		case <-time.After(2 * d.factory.config.RenewalInterval):
		}
	})
}
func TestLinuxFlowQueueLossReportsPartialCoverage(t *testing.T) {
	runLinuxConnectionFixture(t, checkLinuxFlowLoss)
}
func TestLinuxFlowUnclassifiedPacketReportsPartialCoverage(t *testing.T) {
	runLinuxConnectionFixture(t, checkLinuxUnclassifiedFlow, coordinator.ACLPort{Protocol: coordinator.ProtocolAll})
}
func TestLinuxMembershipRevocationClosesNativeSession(t *testing.T) {
	runLinuxConnectionFixture(t, func(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
		session := d.native.(*dataplane.LinuxSession)
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool {
			return local.Applied && local.Permission.NewFlows && local.Permission.ExistingFlows && session.FlowMonitor().Collecting()
		})
		socket, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)}, &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: 443})
		if err != nil {
			t.Fatal(err)
		}
		defer socket.Close()
		if _, err := socket.Write([]byte("before revoke")); err != nil {
			t.Fatal(err)
		}
		select {
		case r := <-f.reports:
			if r.Window.GetDestinationPort() != 443 || r.Window.GetPackets() != 1 {
				t.Fatal("missing admitted application packet", r.Window)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no admitted traffic before revocation")
		}
		close(f.revokeMembership)
		select {
		case <-d.done:
			if !errors.Is(d.result(), connectivity.ErrRevoked) {
				t.Fatal("unexpected terminal result", d.result())
			}
		case <-time.After(2 * time.Second):
			t.Fatal("signed denial did not terminate the connection")
		}
		perm := session.ConnectionLocal().Permission
		if perm.ExistingFlows || perm.NewFlows {
			t.Fatal("revoked session still permits traffic", perm)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := d.Disconnect(ctx); err != nil {
			t.Fatal(err)
		}
		route, err := exec.CommandContext(ctx, "ip", "-4", "route", "show", "exact", "100.64.0.2/32").Output()
		if err != nil || len(route) != 0 {
			t.Fatalf("peer route after teardown: %s, %v", route, err)
		}
		if _, err := socket.Write([]byte("after revoke")); err == nil {
			t.Fatal("old socket retained its peer path")
		}
	})
}

func runLinuxConnectionFixture(t *testing.T, checkFlow func(*testing.T, *connectionDriver, *recoveryGatewayFixture), flowPorts ...coordinator.ACLPort) {
	runLinuxObserverFixture(t, false, checkFlow, flowPorts...)
}

func TestLinuxKernelFlowReportsQuietInterval(t *testing.T) {
	runLinuxObserverFixture(t, true, func(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
		type interval struct{ start, end time.Time }
		classes := make(map[interval]map[connectivity.FilterClass]bool)
		timer := time.NewTimer(8 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				t.Fatal("kernel observation never confirmed a quiet interval for all four classes")
			case w := <-f.reports:
				t.Fatal("unexpected traffic on quiet interface", w.Window.Decision)
			case r := <-f.complete:
				span := interval{r.StartInclusive.AsTime(), r.EndExclusive.AsTime()}
				if !span.end.After(span.start) || r.ConsentVersion != 3 || r.ReportId == "" {
					t.Fatal("invalid quiet report")
				}
				if classes[span] == nil {
					classes[span] = make(map[connectivity.FilterClass]bool)
				}
				classes[span][connectivity.FilterClass{Direction: r.Direction, Decision: r.Decision}] = true
				if len(classes[span]) == 4 {
					return
				}
			}
		}
	})
}

func TestLinuxKernelFlowConsentAndRemoval(t *testing.T) {
	runLinuxObserverFixture(t, true, func(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
		rules := func() []byte {
			out, err := exec.CommandContext(t.Context(), "nft", "list", "ruleset").Output()
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool {
			return local.Applied && bytes.Contains(rules(), []byte("flow_interfaces")) && bytes.Contains(rules(), []byte("expires"))
		})
		for _, port := range []int{443, 22} {
			conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)}, &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: port})
			if err != nil {
				t.Fatal(err)
			}
			_, err = conn.Write([]byte("fixture"))
			conn.Close()
			if err != nil && port == 443 {
				t.Fatal(err)
			}
		}
		expected := map[uint32]string{443: "allow", 22: "deny"}
		windows := make(map[string]*flow.FlowWindow)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for len(expected) > 0 {
			select {
			case r := <-f.reports:
				w := r.Window
				if decision, ok := expected[w.DestinationPort]; ok {
					if w.Decision != decision || w.GetPackets() != 1 || w.GetBytes() != 35 || w.Direction != flow.FlowDirection_FLOW_DIRECTION_EGRESS {
						t.Fatal("incorrect kernel observation", w)
					}
					windows[decision] = w
					delete(expected, w.DestinationPort)
				}
			case <-timer.C:
				t.Fatal("missing kernel observations", expected)
			}
		}
		for len(windows) > 0 {
			select {
			case r := <-f.complete:
				w := windows[r.Decision]
				if w != nil && r.Direction == w.Direction &&
					!r.StartInclusive.AsTime().Before(w.WindowStart.AsTime()) &&
					!r.EndExclusive.AsTime().After(w.WindowEnd.AsTime()) &&
					r.EndExclusive.AsTime().After(r.StartInclusive.AsTime()) {
					delete(windows, r.Decision)
				}
			case <-timer.C:
				t.Fatal("missing complete coverage inside observed traffic windows", windows)
			}
		}
		f.flowDisabled.Store(true)
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool {
			out := rules()
			return local.Applied && !d.native.(*dataplane.LinuxSession).FlowMonitor().Collecting() && !bytes.Contains(out, []byte("flow_interfaces")) && !bytes.Contains(out, []byte("seen_output"))
		})
	}, coordinator.ACLPort{Protocol: "udp", Port: 443})
}

func TestLinuxKernelFlowReportsAuthenticatedIngress(t *testing.T) {
	runLinuxObserverFixture(t, true, func(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
		ctx := t.Context()
		// Keep the peer network namespace alive and signal when nsenter can use it.
		peer := exec.CommandContext(ctx, "unshare", "--net", "--", "sh", "-c", "printf R; exec sleep 40")
		ready, err := peer.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = peer.Process.Kill(); _ = peer.Wait() }()
		if _, err := io.ReadFull(ready, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		pid := strconv.Itoa(peer.Process.Pid)
		run := func(inPeer bool, input string, args ...string) {
			t.Helper()
			if inPeer {
				args = append([]string{"nsenter", "-t", pid, "-n", "--"}, args...)
			}
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Stdin = bytes.NewBufferString(input)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", args[0], err, out)
			}
		}
		run(false, "", "ip", "link", "add", "localunder", "type", "veth", "peer", "name", "peerunder", "netns", pid)
		run(false, "", "ip", "addr", "add", "192.0.2.1/24", "dev", "localunder")
		run(false, "", "ip", "link", "set", "localunder", "up")
		run(true, "", "ip", "addr", "add", "192.0.2.2/24", "dev", "peerunder")
		run(true, "", "ip", "link", "set", "peerunder", "up")
		run(true, "", "ip", "link", "add", "wgfixture", "type", "wireguard")
		key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
		run(true, key+"\n", "wg", "set", "wgfixture", "listen-port", "51820", "private-key", "/dev/stdin", "peer", f.nodePublic, "endpoint", "192.0.2.1:51820", "allowed-ips", "100.64.0.1/32")
		run(true, "", "ip", "addr", "add", "100.64.0.2/30", "dev", "wgfixture")
		run(true, "", "ip", "link", "set", "wgfixture", "up")

		var sockets []*net.UDPConn
		for _, port := range []int{5300, 5301} {
			socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1), Port: port})
			if err != nil {
				t.Fatal(err)
			}
			defer socket.Close()
			sockets = append(sockets, socket)
		}
		waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool {
			out, err := exec.CommandContext(ctx, "nft", "list", "ruleset").Output()
			return local.Applied && err == nil && bytes.Contains(out, []byte("expires"))
		})
		run(true, "", "python3", "-c", "import socket; s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); [s.sendto(b'fixture',('100.64.0.1',p)) for p in (5300,5301)]")
		var buf [16]byte
		if err := sockets[0].SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if n, _, err := sockets[0].ReadFromUDP(buf[:]); err != nil || string(buf[:n]) != "fixture" {
			t.Fatalf("allowed ingress missing: %v", err)
		}
		want := map[uint32]string{5300: "allow", 5301: "deny"}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for len(want) > 0 {
			select {
			case r := <-f.reports:
				w := r.Window
				if decision, ok := want[w.DestinationPort]; ok {
					if w.Direction != flow.FlowDirection_FLOW_DIRECTION_INGRESS || w.Decision != decision || w.Source != "100.64.0.2" || w.Destination != "100.64.0.1" || w.GetPackets() != 1 || w.GetBytes() != 35 {
						t.Fatal("incorrect ingress window", w)
					}
					delete(want, w.DestinationPort)
				}
			case <-timer.C:
				t.Fatal("missing ingress reports", want)
			}
		}
		if err := sockets[1].SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := sockets[1].ReadFromUDP(buf[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("denied ingress reached socket: %v", err)
		}
	})
}

func runLinuxObserverFixture(t *testing.T, kernelFlow bool, checkFlow func(*testing.T, *connectionDriver, *recoveryGatewayFixture), flowPorts ...coordinator.ACLPort) {
	withFlow := checkFlow != nil
	if testing.Short() || os.Geteuid() != 0 {
		t.Skip("requires root, iproute2 and nftables in an isolated network namespace")
	}
	const marker = "EN_TEST_BOOTSTRAP_NETWORK_NAMESPACE"
	if os.Getenv(marker) != t.Name() {
		ctx, stop := context.WithTimeout(t.Context(), 40*time.Second)
		defer stop()
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(ctx, "unshare", "--net", "--", binary, "-test.run=^"+t.Name()+"$")
		child.Env = append(os.Environ(), marker+"="+t.Name())
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("isolated Linux recovery: %v\n%s", err, output)
		}
		return
	}
	if output, err := exec.CommandContext(t.Context(), "ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Fatalf("enable namespace loopback: %v: %s", err, output)
	}
	now := time.Now().UTC()
	network, networkKey := recoveryFixtureBundle(61, now)
	node, nodeKey := recoveryFixtureBundle(62, now)
	relay, _ := recoveryFixtureBundle(63, now)
	root, rootKey := recoveryFixtureBundle(64, now)
	_, foreignKey := recoveryFixtureBundle(65, now)
	bundles := signing.ServerKeyResponse{TrustBundle: network, NodeCredentialTrustBundle: node, RelayTrustBundle: relay}
	fixture := &recoveryGatewayFixture{mapKey: networkKey, mapKeyID: network.ActiveKeyID, streams: make(chan chan struct{}, 8), revokeMembership: make(chan struct{})}
	mux := http.NewServeMux()
	mux.Handle(signingapiconnect.NewBootstrapServiceHandler(fixture))
	mux.Handle(publicqueryv1connect.NewConnectivityServiceHandler(fixture))
	if withFlow {
		fixture.kernelFlow = kernelFlow
		if len(flowPorts) == 0 {
			for port := 443; port <= 451; port++ {
				flowPorts = append(flowPorts, coordinator.ACLPort{Protocol: "udp", Port: port})
			}
		}
		fixture.flowPorts = flowPorts
		fixture.reports = make(chan *flow.ReportFlowLogRequest, 8)
		fixture.complete = make(chan *flow.ReportFlowCoverageRequest, 16)
		fixture.coverage = make(chan *flow.ReportFlowCoverageRequest, 4)
		fixture.flowStart = now
		mux.Handle(publicv1connect.NewFlowLogServiceHandler(fixture))
	}
	gateway := httptest.NewUnstartedServer(mux)
	gateway.EnableHTTP2 = true
	gateway.StartTLS()
	t.Cleanup(gateway.Close)
	raw, err := json.Marshal(bundles)
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := control.DecodeAnnouncement(gateway.URL, raw, now)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := workerStorePrepared(t, func(p *state.Profile) {
		p.ControlOrigin, p.Attempt.ControlOrigin = gateway.URL, gateway.URL
		p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
		var extra []string
		if withFlow {
			extra = []string{coordinator.NodeFlowScope}
		}
		p.Secrets.NodeCredential = recoveryFixtureCredential(t, p, node.ActiveKeyID, nodeKey, extra...)
		fixture.credential = p.Secrets.NodeCredential
		key, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			t.Fatal(err)
		}
		fixture.nodePublic = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	})
	metadata := signing.BootstrapMetadata{Origin: gateway.URL, Revision: 1, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Bundles: bundles}
	fixture.bootstrap = recoveryFixtureBootstrap(t, metadata, root.ActiveKeyID, rootKey)
	metadata.Revision++
	fixture.foreign = recoveryFixtureBootstrap(t, metadata, root.ActiveKeyID, foreignKey)
	clock := workerFixtureClock{}
	if err := workerIntent(t, store, state.IntentConnected, now, workerRuntime); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := control.NewVerifiedBootstrap(gateway.Client(), 65536, time.Now, map[string]signing.BootstrapRoot{
		gateway.URL: {KeyID: root.ActiveKeyID, PublicKey: rootKey.Public().(ed25519.PublicKey)},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := connectivity.NewTransport(gateway.Client(), connectivity.HTTPConfig{
		CallTimeout: time.Second, StreamDuration: 15 * time.Second, ReadIdleTimeout: 10 * time.Second,
		MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 8, MaxCalls: 2, MaxStreams: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.CloseIdleConnections)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(directory)
	resources, err := dataplane.NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	factory := connectionFactory{
		store: store, transport: transport, stream: transport.StreamSnapshot,
		cleanup: func(ctx context.Context, _ string) error { return dataplane.CleanLinuxNetwork(ctx, ledger) },
		config: connectionConfig{
			Verifier: connectivity.VerifierConfig{Announcements: bootstrap, Clock: clock, Now: time.Now, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192},
			Endpoint: netip.MustParseAddrPort("192.0.2.1:51820"), EndpointTTL: time.Minute,
			RenewalInterval: 20 * time.Second, RetryInterval: 80 * time.Millisecond, ProbeInterval: 100 * time.Millisecond,
			WriteBudget: time.Second, ApplyTimeout: 2 * time.Second, ListenPort: fixtureTunnelPort, MaxQueuedMaps: 8,
			ReportFailure: func(_ uint64, stage string, err error) { t.Logf("%s: %v", stage, err) },
		},
		makeNative: func(ctx context.Context, profile string, generation uint64) (nativeConnectionSession, error) {
			cfg := dataplane.LinuxSessionConfig{
				MTU: 1420, MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, ListenPort: fixtureTunnelPort,
				ProbeInterval: time.Second, RetryInterval: time.Second,
				Relay: dataplane.RelayConfig{DialTimeout: time.Second, IOTimeout: 3 * time.Second, HeartbeatInterval: time.Second, Now: time.Now},
			}
			if kernelFlow {
				cfg.FlowCapacity, cfg.FlowDumpBytes = 8, 65536
			}
			return dataplane.NewLinuxSession(ctx, store, ledger, clock, profile, generation, cfg, resources)
		},
	}
	if withFlow {
		factory.config.RenewalInterval = 200 * time.Millisecond
	}
	owner, err := factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	driver := owner.(*connectionDriver)
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := driver.Disconnect(ctx); err != nil {
			t.Error(err)
		}
	})
	sample, err := clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	budget, err := connectivity.NewOperationBudget(clock, workerProfile, 1, sample, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if err := driver.Connect(connectivity.WithOperationBudget(ctx, budget), false); err != nil {
		t.Fatalf("initial Linux Connect: %v", err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}

	if withFlow {
		checkFlow(t, driver, fixture)
		return
	}
	for _, long := range []bool{false, true} {
		initial := waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return local.Known && local.Applied && local.Permission.NewFlows
		})
		if !initial.Applied || !initial.Permission.NewFlows {
			t.Fatalf("Linux tunnel never became ready: %#v", initial)
		}
		fixture.mode.Store(http.StatusServiceUnavailable)
		close(recoveryFixtureStream(t, fixture.streams))
		offline := waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return fixture.unavailable.Load() > 0 && local.Known && local.ControlUnavailable && !local.Permission.NewFlows && local.Permission.ExistingFlows
		})
		if !offline.Permission.ExistingFlows || !offline.Permission.ExpiresAt.Equal(initial.Permission.ExpiresAt) {
			t.Fatalf("outage changed the original lease: %#v", offline)
		}
		if long {
			waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
				return !time.Now().Before(initial.Permission.ExpiresAt) && local.Known && local.ControlUnavailable && !local.Permission.NewFlows && !local.Permission.ExistingFlows
			})
		}
		fixture.mode.Store(http.StatusOK)
		waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return local.Applied && !local.ControlUnavailable && local.Permission.NewFlows && local.MapRevision > initial.MapRevision
		})
		fixture.unavailable.Store(0)
	}

	fixture.mode.Store(http.StatusForbidden)
	close(recoveryFixtureStream(t, fixture.streams))
	select {
	case <-driver.done:
		if err := driver.result(); !errors.Is(err, connectivity.ErrTrust) || driver.ConnectionLocal().Permission.ExistingFlows {
			t.Fatalf("foreign signing identity did not fail closed: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("foreign signing identity did not terminate the connection")
	}
}

func recoveryFixtureStream(t *testing.T, streams <-chan chan struct{}) chan struct{} {
	t.Helper()
	select {
	case stream := <-streams:
		return stream
	case <-time.After(time.Second):
		t.Fatal("no established map stream to interrupt")
		return nil
	}
}

func waitRecoveryObservation(t *testing.T, driver *connectionDriver, ready func(connectivity.LocalObservation) bool) connectivity.LocalObservation {
	t.Helper()
	timeout := time.NewTimer(7 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		local := driver.ConnectionLocal()
		if ready(local) {
			return local
		}
		select {
		case <-driver.done:
			t.Fatalf("Linux tunnel stopped instead of recovering: %v", driver.result())
		case <-timeout.C:
			t.Fatalf("Linux recovery did not reach the expected state: %#v", local)
		case <-tick.C:
		}
	}
}

type recoveryGatewayFixture struct {
	signingapiconnect.UnimplementedBootstrapServiceHandler
	publicqueryv1connect.UnimplementedConnectivityServiceHandler
	publicv1connect.UnimplementedFlowLogServiceHandler
	credential        string
	flowStart         time.Time
	kernelFlow        bool
	flowPorts         []coordinator.ACLPort
	flowDisabled      atomic.Bool
	flowPolicy        atomic.Pointer[flow.GetFlowLogPolicyResponse]
	policyUnavailable atomic.Bool
	reports           chan *flow.ReportFlowLogRequest
	complete          chan *flow.ReportFlowCoverageRequest
	coverage          chan *flow.ReportFlowCoverageRequest
	coverageCalls     atomic.Uint32
	holdWindows       atomic.Bool
	reportCalls       atomic.Uint32

	mapKey               ed25519.PrivateKey
	mapKeyID, nodePublic string
	bootstrap, foreign   *signingwire.GetBootstrapResponse
	revokeMembership     chan struct{}
	streams              chan chan struct{}
	mode                 atomic.Int32
	unavailable          atomic.Uint64
	revision             atomic.Uint64
}

func (f *recoveryGatewayFixture) GetBootstrap(_ context.Context, _ *connect.Request[signingwire.GetBootstrapRequest]) (*connect.Response[signingwire.GetBootstrapResponse], error) {
	switch f.mode.Load() {
	case http.StatusServiceUnavailable:
		f.unavailable.Add(1)
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fixture bootstrap unavailable"))
	case http.StatusForbidden:
		return connect.NewResponse(f.foreign), nil
	default:
		return connect.NewResponse(f.bootstrap), nil
	}
}

func (f *recoveryGatewayFixture) UpdateNodeEndpoint(_ context.Context, req *connect.Request[wire.UpdateNodeEndpointRequest]) (*connect.Response[wire.UpdateNodeEndpointResponse], error) {
	now := time.Now().UTC()
	input := req.Msg
	return connect.NewResponse(&wire.UpdateNodeEndpointResponse{
		NodeId: "node", NetworkId: "network", Endpoint: input.Endpoint, Generation: input.Generation,
		Online: input.Status == "online", Transport: input.Transport,
		UpdatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(input.Ttl.AsDuration())),
		Revision: &wire.MapRevision{Network: f.revision.Load()},
	}), nil
}

func (f *recoveryGatewayFixture) WatchMaps(ctx context.Context, req *connect.Request[wire.WatchMapsRequest], stream *connect.ServerStream[wire.WatchMapsResponse]) error {
	now := time.Now().UTC()
	revision := f.revision.Add(1)
	value := coordinator.NetworkMapSnapshot{
		Revision: coordinator.MapRevision{Network: revision},
		Network:  coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.64.0.0/24", Revision: revision},
		Node:     coordinator.Node{ID: "node", NetworkID: "network", PublicKey: f.nodePublic, AssignedIP: "100.64.0.1", ApprovalState: "approved", Status: "online"},
		Peers:    []coordinator.Peer{},
	}
	expiry := now.Add(3 * time.Second)
	if f.reports != nil {
		key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{42}, 32))
		if err != nil {
			return err
		}
		value.Peers = []coordinator.Peer{{ID: "peer", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), AllowedIPs: []string{"100.64.0.2/32"}, Endpoint: "192.0.2.2:51820", EndpointGeneration: 1, EndpointExpiresAt: &expiry, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}, AllowedPorts: f.flowPorts}}}}
		if f.kernelFlow {
			expiry = now.Add(20 * time.Second)
			value.Peers[0].IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.1/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 5300}}}}
		}
	}
	signature, err := f.signMap(&value, now, expiry)
	if err != nil {
		return err
	}
	proof := &coordinator.MapTimeProof{Nonce: req.Msg.Nonce, MapHash: signature.PayloadHash, ServerTime: now}
	message, err := coordinator.MapTimeProofSigningPayload(*proof, *signature)
	if err != nil {
		return err
	}
	proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapKey, message))
	event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"},
		EventID: "map:" + signature.PayloadHash, To: value.Revision, Snapshot: &value, ResultSignature: signature, TimeProof: proof}
	if err := stream.Send(testwire.Update(event)); err != nil {
		return err
	}
	cut := make(chan struct{})
	select {
	case f.streams <- cut:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-f.revokeMembership:
		value.Node.Status = "revoked"
		value.Peers = []coordinator.Peer{}
		value.MapSignature = nil
		revoked, err := f.signMap(&value, time.Now().UTC(), expiry)
		if err != nil {
			return err
		}
		event.Type, event.From, event.ResultSignature, event.TimeProof = "resync", value.Revision, revoked, nil
		event.EventID = "map:" + revoked.PayloadHash
		if err := stream.Send(testwire.Update(event)); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	case <-cut:
		return connect.NewError(connect.CodeUnavailable, errors.New("fixture stream interrupted"))
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *recoveryGatewayFixture) signMap(value *coordinator.NetworkMapSnapshot, now, expiry time.Time) (*coordinator.MapSignature, error) {
	payload, err := coordinator.NetworkMapSigningPayload(*value, f.mapKeyID, now, expiry)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	signature := &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: f.mapKeyID, IssuedAt: now, ExpiresAt: expiry,
		PayloadHash: hex.EncodeToString(digest[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapKey, payload))}
	value.MapSignature = signature
	return signature, nil
}

func recoveryFixtureBundle(seed byte, now time.Time) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	id := "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:])
	before, after := now.Add(-time.Hour), now.Add(2*time.Hour)
	return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{
		{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public), NotBefore: &before, NotAfter: &after},
	}}, private
}

func recoveryFixtureBootstrap(t *testing.T, metadata signing.BootstrapMetadata, keyID string, key ed25519.PrivateKey) *signingwire.GetBootstrapResponse {
	t.Helper()
	raw, err := signing.BootstrapSigningBytes(metadata, keyID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := signing.NewBootstrapResponse(metadata, keyID, ed25519.Sign(key, raw))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func recoveryFixtureCredential(t *testing.T, profile *state.Profile, keyID string, key ed25519.PrivateKey, extra ...string) string {
	t.Helper()
	p := profile.Attempt
	scopes := append([]string{"node:register", "node:map", "node:endpoint"}, extra...)
	raw, err := coordinator.NodeCredentialSigningPayload(keyID, p.NetworkID, p.NodeID, scopes, p.CredentialEnds)
	if err != nil {
		t.Fatal(err)
	}
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: keyID, NetworkID: p.NetworkID, NodeID: p.NodeID, Scopes: scopes,
		ExpiresAt: p.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw))}
	raw, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "enc_" + base64.RawURLEncoding.EncodeToString(raw)
}

func (f *recoveryGatewayFixture) GetFlowLogPolicy(_ context.Context, r *connect.Request[flow.GetFlowLogPolicyRequest]) (*connect.Response[flow.GetFlowLogPolicyResponse], error) {
	if r.Msg.NodeId != "node" || r.Header().Get("X-EndlessNet-Node-Credential") != f.credential {
		return nil, connect.NewError(connect.CodePermissionDenied, nil)
	}
	if f.policyUnavailable.Load() {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	}
	if p := f.flowPolicy.Load(); p != nil {
		return connect.NewResponse(p), nil
	}
	if f.flowDisabled.Load() {
		return connect.NewResponse(&flow.GetFlowLogPolicyResponse{ConsentVersion: 4}), nil
	}
	return connect.NewResponse(&flow.GetFlowLogPolicyResponse{ConsentVersion: 3, CollectionNotBefore: timestamppb.New(f.flowStart.Add(-time.Second)), CollectionExpiresAt: timestamppb.New(f.flowStart.Add(time.Minute))}), nil
}
func (f *recoveryGatewayFixture) ReportFlowLog(ctx context.Context, r *connect.Request[flow.ReportFlowLogRequest]) (*connect.Response[flow.ReportFlowLogResponse], error) {
	if r.Msg.NodeId != "node" || r.Header().Get("X-EndlessNet-Node-Credential") != f.credential {
		return nil, connect.NewError(connect.CodePermissionDenied, nil)
	}
	select {
	case f.reports <- r.Msg:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.reportCalls.Add(1) == 1 || f.holdWindows.Load() {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	}
	return connect.NewResponse(&flow.ReportFlowLogResponse{WindowId: r.Msg.Window.WindowId}), nil
}

func (f *recoveryGatewayFixture) ReportFlowCoverage(ctx context.Context, r *connect.Request[flow.ReportFlowCoverageRequest]) (*connect.Response[flow.ReportFlowCoverageResponse], error) {
	if r.Msg.NodeId != "node" || r.Header().Get("X-EndlessNet-Node-Credential") != f.credential {
		return nil, connect.NewError(connect.CodePermissionDenied, nil)
	}
	if r.Msg.Coverage == flow.FlowCoverage_FLOW_COVERAGE_COMPLETE {
		select {
		case f.complete <- r.Msg:
		default:
		}
		return connect.NewResponse(&flow.ReportFlowCoverageResponse{ReportId: r.Msg.ReportId}), nil
	}
	select {
	case f.coverage <- r.Msg:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.coverageCalls.Add(1) == 1 {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	}
	return connect.NewResponse(&flow.ReportFlowCoverageResponse{ReportId: r.Msg.ReportId}), nil
}

func checkLinuxFlowLoss(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
	t.Helper()
	f.holdWindows.Store(true)
	monitor := d.native.(*dataplane.LinuxSession).FlowMonitor()
	waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return monitor.Collecting() && local.Applied })
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	for port := 443; port <= 451; port++ {
		if _, err := conn.WriteToUDP([]byte("fixture"), &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: port}); err != nil {
			t.Fatal(err)
		}
	}
	checkLinuxPartialReceipt(t, d, f, start)
}

func checkLinuxUnclassifiedFlow(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
	t.Helper()
	monitor := d.native.(*dataplane.LinuxSession).FlowMonitor()
	waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return monitor.Collecting() && local.Applied })
	conn, err := net.DialIP("ip4:icmp", &net.IPAddr{IP: net.IPv4(100, 64, 0, 1)}, &net.IPAddr{IP: net.IPv4(100, 64, 0, 2)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	// ICMP echo request with zero identifier and sequence, including checksum.
	if _, err := conn.Write([]byte{8, 0, 0xf7, 0xff, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	checkLinuxPartialReceipt(t, d, f, start)
	if len(f.reports) != 0 {
		t.Fatal("emitted a TCP/UDP window for an ICMP packet")
	}
}

func checkLinuxPartialReceipt(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture, start time.Time) {
	t.Helper()
	receive := func() *flow.ReportFlowCoverageRequest {
		t.Helper()
		select {
		case r := <-f.coverage:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("missing native partial coverage report")
			return nil
		}
	}
	r := receive()
	if r.ReportId == "" || r.ConsentVersion != 3 || r.Direction != flow.FlowDirection_FLOW_DIRECTION_EGRESS || r.Decision != "allow" || r.Coverage != flow.FlowCoverage_FLOW_COVERAGE_PARTIAL || !r.StartInclusive.AsTime().Before(r.EndExclusive.AsTime()) || r.EndExclusive.AsTime().Before(start) {
		t.Fatal("invalid coverage for observed packet loss", r)
	}
	if !proto.Equal(r, receive()) {
		t.Fatal("lost acknowledgement changed the reported interval")
	}
	select {
	case <-f.coverage:
		t.Fatal("reported an acknowledged loss again without new traffic")
	case <-time.After(2 * d.factory.config.RenewalInterval):
	}
}
func checkLinuxPendingRevocation(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
	t.Helper()
	f.holdWindows.Store(true)
	m := d.native.(*dataplane.LinuxSession).FlowMonitor()
	waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return m.Collecting() && local.Applied })
	conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)}, &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("pending")); err != nil {
		t.Fatal(err)
	}
	var first *flow.ReportFlowLogRequest
	for attempts := 0; attempts < 2; {
		select {
		case r := <-f.reports:
			if first == nil {
				first = r
			} else if !proto.Equal(first, r) {
				t.Fatal("retry changed pending window")
			}
			attempts++
		case <-time.After(2 * time.Second):
			t.Fatal("no retry of pending native window")
		}
	}
	f.flowDisabled.Store(true)
	waitRecoveryObservation(t, d, func(connectivity.LocalObservation) bool { return !m.Collecting() })
	calls := f.reportCalls.Load()
	f.holdWindows.Store(false)
	time.Sleep(3 * d.factory.config.RenewalInterval)
	if f.reportCalls.Load() != calls {
		t.Fatal("reported pending traffic after consent revocation")
	}
}

func checkLinuxFlow(t *testing.T, d *connectionDriver, f *recoveryGatewayFixture) {
	t.Helper()
	m := d.native.(*dataplane.LinuxSession).FlowMonitor()
	waitRecoveryObservation(t, d, func(local connectivity.LocalObservation) bool { return m.Collecting() && local.Applied })
	conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1)}, &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send := func() {
		t.Helper()
		if _, err := conn.Write([]byte("fixture")); err != nil {
			t.Fatal(err)
		}
	}
	receive := func() *flow.ReportFlowLogRequest {
		t.Helper()
		select {
		case r := <-f.reports:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("native UDP window missing")
			return nil
		}
	}
	send()
	first := receive()
	w := first.Window
	if first.ConsentVersion != 3 || w == nil || w.WindowId == "" || w.Source != "100.64.0.1" || w.Destination != "100.64.0.2" || w.DestinationPort != 443 || w.Direction != flow.FlowDirection_FLOW_DIRECTION_EGRESS || w.Protocol != "udp" || w.Decision != "allow" || w.GetPackets() != 1 || w.GetBytes() != 35 {
		t.Fatal("wrong native egress observation", first)
	}
	if again := receive(); !proto.Equal(first, again) {
		t.Fatal("retry changed original observation")
	}
	f.flowDisabled.Store(true)
	waitRecoveryObservation(t, d, func(connectivity.LocalObservation) bool { return !m.Collecting() })
	send()
	select {
	case <-f.reports:
		t.Fatal("reported traffic with consent disabled")
	case <-time.After(2 * d.factory.config.RenewalInterval):
	}
}
