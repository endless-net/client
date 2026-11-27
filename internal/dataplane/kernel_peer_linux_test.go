package dataplane

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
)

func TestKernelAWGRequiresExactReadbackOfEveryProfileField(t *testing.T) {
	want := kernelAWGSettings(cipherProfile(transport.AmneziaWG).AWG)
	check := func(attrs []netlink.Attribute) error {
		raw, err := netlink.MarshalAttributes(attrs)
		if err != nil {
			t.Fatal(err)
		}
		return verifyKernelAWGSettings([]genetlink.Message{{Data: raw}}, want)
	}
	if err := check(want); err != nil {
		t.Fatal(err)
	}
	for i, field := range want {
		rest := append([]netlink.Attribute{}, want[:i]...)
		rest = append(rest, want[i+1:]...)
		if check(rest) == nil {
			t.Fatalf("accepted missing attribute %d", field.Type)
		}
		changed := netlink.Attribute{Type: field.Type, Data: bytes.Clone(field.Data)}
		changed.Data[0] ^= 1
		if check(append(rest, changed)) == nil {
			t.Fatalf("accepted changed attribute %d", field.Type)
		}
		if check(append(rest, field, field)) == nil {
			t.Fatalf("accepted duplicate attribute %d", field.Type)
		}
	}
}

func TestLinuxKernelPeerExchangesDataWithGo(t *testing.T) {
	testKernelPeerExchangesData(t, transport.WireGuard)
}

func TestLinuxKernelAmneziaPeerExchangesDataWithGo(t *testing.T) {
	testKernelPeerExchangesData(t, transport.AmneziaWG)
}

func TestLinuxPrivateKernelDevicesCarryExactIP(t *testing.T) {
	testPrivateKernelDevicesCarryExactIP(t, transport.WireGuard)
}

func TestLinuxPrivateAmneziaDevicesCarryExactIP(t *testing.T) {
	testPrivateKernelDevicesCarryExactIP(t, transport.AmneziaWG)
}

func testPrivateKernelDevicesCarryExactIP(t *testing.T, engine string) {
	t.Helper()
	if !linuxNetworkTest(t) {
		return
	}
	first := newCandidateEngineFixture(t, engine)
	second := reverseSearchFixture(t, first)
	var peers []*linuxPeerDevice
	var workers sync.WaitGroup
	var binds []*candidateBind
	var sockets []*searchSocket
	var destinations []awgconn.Endpoint
	defer func() {
		for _, b := range binds {
			b.shutdown()
		}
		for _, peer := range peers {
			peer.Close()
		}
		workers.Wait()
	}()
	for _, f := range []*candidateFixture{first, second} {
		sample, err := f.clock.Sample()
		if err != nil {
			t.Fatal(err)
		}
		a, err := newCandidateAssignment(f.verified, f.lease, f.snapshot.Peers[0].ID, "fixture-profile", sample.Wall)
		if err != nil {
			t.Fatal(err)
		}
		d, err := openLinuxPeerDevice(t.Context(), directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, mtu: 1420, clock: f.clock})
		if err != nil {
			t.Fatal(err)
		}
		peers = append(peers, d)
		b, err := newCandidateBind(t.Context(), a, f.clock)
		if err != nil {
			t.Fatal(err)
		}
		binds = append(binds, b)
		sock := &searchSocket{newCandidateSocketFixture(), netip.MustParseAddrPort(a.localEndpoint)}
		sockets = append(sockets, sock)
		b.listen = func(uint16) (candidateSocket, error) { return sock, nil }
		rx, _, err := b.Open(0)
		if err != nil {
			t.Fatal(err)
		}
		dst, err := b.ParseEndpoint(a.peerEndpoint)
		if err != nil {
			t.Fatal(err)
		}
		destinations = append(destinations, dst)
		cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, a.plan.LocalIntentGeneration, "native")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cycle.cancel)
		attempt, err := cycle.attempt("native")
		if err != nil {
			t.Fatal(err)
		}
		if err := b.beginProbe(attempt); err != nil {
			t.Fatal(err)
		}
		workers.Go(func() { _ = d.receiveCandidate(rx[0]) })
	}
	if _, err := net.InterfaceByName("en-peer"); err == nil {
		t.Fatal("candidate escaped into owner namespace")
	}
	var outputs [2]*linuxPeerOutput
	atExternalSend := make(chan chan struct{}, 1)
	for i, src := range peers {
		sender, receiver := sockets[i], sockets[1-i]
		sender.forward = func(packet candidateDatagram) error {
			if packet.address != receiver.address {
				return ErrNoAuthority
			}
			select {
			case receiver.reads <- candidateDatagram{packet.raw, sender.address}:
				return nil
			case <-receiver.done:
				return net.ErrClosed
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		out := newLinuxPeerOutput(ctx, src)
		outputs[i] = out
		workers.Go(func() {
			_ = out.forward(func(raw []byte, guard func(int) error) error {
				if i == 0 && guard != nil {
					select {
					case signal := <-atExternalSend:
						close(signal)
					default:
					}
				}
				return binds[i].send([][]byte{raw}, destinations[i], guard)
			})
		})

	}
	for i, f := range []*candidateFixture{first, second} {
		dst := peers[1-i]
		ip, err := netip.ParsePrefix(f.snapshot.Peers[0].AllowedIPs[0])
		if err != nil {
			t.Fatal(err)
		}
		payload := cipherIPv4(f.snapshot.Node.AssignedIP, ip.Addr().String(), byte(i+1))
		out := outputs[i]
		denied := bytes.Clone(payload)
		denied[len(denied)-1] ^= 1
		if err := out.inject(denied, func(int) error { return ErrNoAuthority }); !errors.Is(err, ErrNoAuthority) {
			t.Fatalf("native external write accepted expired authority: %v", err)
		}
		var charged int
		if err := out.inject(payload, func(size int) error { charged = size; return nil }); err != nil {
			t.Fatal(err)
		}
		sockets[i].mu.Lock()
		last := sockets[i].writes[len(sockets[i].writes)-1]
		sockets[i].mu.Unlock()
		if charged != len(last.raw) || charged <= len(payload) {
			t.Fatalf("guard observed %d bytes, wrote %d", charged, len(last.raw))
		}
		if err := dst.plain.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var packet [2048]byte
		n, err := dst.plain.Read(packet[:])
		if err != nil {
			t.Fatal(err)
		}
		frame, measured, err := splitKernelIPv4Frame(packet[:n])
		if err != nil || !bytes.Equal(frame, payload) {
			t.Fatalf("peer %d, received %d bytes: %v", i, n, err)
		}
		sockets[i].mu.Lock()
		observed := len(sockets[i].writes[len(sockets[i].writes)-1].raw)
		sockets[i].mu.Unlock()
		if measured != observed {
			t.Fatalf("ingress measured %d; socket sent %d", measured, observed)
		}
	}
	var observed int
	for _, sock := range sockets {
		sock.mu.Lock()
		for _, packet := range sock.writes {
			observed += len(packet.raw) + 28
		}
		sock.mu.Unlock()
	}
	for _, b := range binds {
		if used := b.probe.cycle.wire.consumed(); used != observed || observed == 0 {
			t.Fatalf("discovery charged %d, observed %d bytes on the wire", used, observed)
		}
	}
	testReorderedNativeFrames(t, first, second, peers[1], outputs[0], sockets[0], sockets[1])
	testRevokedNativeSend(t, first, outputs[0], binds[0], sockets[0], atExternalSend)
}

func testReorderedNativeFrames(t *testing.T, first, second *candidateFixture, dst *linuxPeerDevice, out *linuxPeerOutput, sender, receiver *searchSocket) {
	t.Helper()
	local, peer, err := first.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	control, err := controlPacket(local, peer, challengeControl(t, first))
	if err != nil {
		t.Fatal(err)
	}
	var frames [][]byte
	for i := 0; i < 12; i++ {
		raw := cipherIPv4(local.String(), peer.String(), byte(i))
		raw = append(raw, bytes.Repeat([]byte{byte(i)}, i*11)...)
		binary.BigEndian.PutUint16(raw[2:4], uint16(len(raw)))
		binary.BigEndian.PutUint16(raw[24:26], uint16(len(raw)-20))
		clear(raw[10:12])
		binary.BigEndian.PutUint16(raw[10:12], packetChecksum(raw[:20], 0))
		frames = append(frames, raw)
		if i == 5 {
			frames = append(frames, control)
		}
	}
	// Hold the simulated network after encryption. The socket still records
	// its actual writes, independent of the authenticated receive metadata.
	sender.mu.Lock()
	sender.forward = func(candidateDatagram) error { return nil }
	sender.mu.Unlock()
	var wire [][]byte
	for _, frame := range frames {
		var size int
		if err := out.inject(frame, func(n int) error { size = n; return nil }); err != nil {
			t.Fatal(err)
		}
		sender.mu.Lock()
		raw := sender.writes[len(sender.writes)-1].raw
		sender.mu.Unlock()
		if len(raw) != size {
			t.Fatalf("guard %d, socket %d", size, len(raw))
		}
		wire = append(wire, raw)
	}
	deliver := func(raw []byte) {
		t.Helper()
		select {
		case receiver.reads <- candidateDatagram{raw, sender.address}:
		case <-time.After(time.Second):
			t.Fatal("receiver stopped")
		}
	}
	var buf [2048]byte
	// Reserve the first frame for the subsequent revocation check.
	for i := len(wire) - 1; i > 0; i-- {
		deliver(wire[i])
		if i == len(wire)-1 {
			deliver(wire[i])
		}
	}
	for i := len(wire) - 1; i > 0; i-- {
		if err := dst.plain.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		n, err := dst.plain.Read(buf[:])
		if err != nil {
			t.Fatal(err)
		}
		frame, size, err := splitKernelIPv4Frame(buf[:n])
		if err != nil || !bytes.Equal(frame, frames[i]) || size != len(wire[i]) {
			t.Fatalf("out-of-order frame %d, wire %d, metadata %d: %v", i, len(wire[i]), size, err)
		}
	}
	second.lease.BlockSource()
	deliver(wire[0])
	if err := dst.plain.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if n, err := dst.plain.Read(buf[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("received %d bytes after replay or loss of authority: %v", n, err)
	}

}

// Revoke after encryption, while the native output worker is waiting for the
// public socket. Verify that even its first post-revocation write is fenced.
func testRevokedNativeSend(t *testing.T, f *candidateFixture, out *linuxPeerOutput, b *candidateBind, sock *searchSocket, notify chan<- chan struct{}) {
	t.Helper()
	local, peer, err := f.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	ip := cipherIPv4(local.String(), peer.String(), 91)
	sock.mu.Lock()
	before := len(sock.writes)
	sock.mu.Unlock()
	<-b.sendGate
	release := sync.OnceFunc(func() { b.sendGate <- struct{}{} })
	defer release()
	atGate := make(chan struct{})
	notify <- atGate
	done := make(chan error, 1)
	go func() { done <- out.inject(ip, func(int) error { return nil }) }()
	select {
	case <-atGate:
	case <-time.After(time.Second):
		t.Fatal("ciphertext did not reach the public send gate")
	}
	f.lease.BlockSource()
	release()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoAuthority) {
			t.Fatalf("queued send returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked send stuck")
	}
	sock.mu.Lock()
	defer sock.mu.Unlock()
	if len(sock.writes) != before {
		t.Fatal("revoked encrypted packet escaped")
	}
}

func testKernelPeerExchangesData(t *testing.T, engine string) {
	t.Helper()
	if !linuxNetworkTest(t) {
		return
	}
	ctx := t.Context()
	f := newCandidateEngineFixture(t, engine)
	a := f.assignment(t)
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "lo", "up")
	runLinuxTestCommand(t, ctx, linuxIP, "link", "add", "candidate", "type", engine)
	iface, err := net.InterfaceByName("candidate")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan []byte, 1)
	port, err := newPacketPort(ctx, 1420, func() connectivity.Permission {
		return connectivity.Permission{ExistingFlows: true, NewFlows: true}
	}, func(packet []byte) error {
		select {
		case received <- bytes.Clone(packet):
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = port.Close() }()
	configured := make(chan struct{})
	var remote wireGuardDevice
	if engine == transport.AmneziaWG {
		tunnel := &amneziaPort{packetPort: port, configured: configured, expectedOffset: awgdevice.MessageTransportHeaderSize + int(a.offer.Profile.AWG.Padding[3])}
		remote = awgdevice.NewDevice(tunnel, awgconn.NewStdNetBind(), awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	} else {
		remote = device.NewDevice(wireGuardPort{port}, conn.NewStdNetBind(), device.NewLogger(device.LogLevelSilent, ""))
	}
	defer remote.Close()
	reverse := coordinator.Peer{PublicKey: a.local.PublicKey, ACLRestricted: true, AllowedIPs: []string{local.String() + "/32"}}
	conn, err := genetlink.Dial(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	if err := configureKernelWGPeer(ctx, conn, iface.Index, netip.MustParseAddrPort("127.0.0.1:39101"), cfg); err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "address", "add", local.String()+"/32", "dev", "candidate")
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "candidate", "up")
	kernelPort, err := kernelListenPort(conn, engine, iface.Index)
	if err != nil {
		t.Fatal(err)
	}
	reverse.Endpoint = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), kernelPort).String()
	config, err := cipherConfiguration(coordinator.Node{PublicKey: a.peer.PublicKey, AssignedIP: peer.String()}, reverse, bytes.Repeat([]byte{84}, 32), a.offer.Profile)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(config)
	if err := remote.IpcSetOperation(bytes.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	if err := remote.IpcSetOperation(bytes.NewBufferString("listen_port=39101\n")); err != nil {
		t.Fatal(err)
	}
	close(configured)
	if err := remote.Up(); err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "route", "add", peer.String()+"/32", "dev", "candidate")
	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: peer.AsSlice(), Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	message := []byte("kernel to Go")
	if _, err := sender.Write(message); err != nil {
		t.Fatal(err)
	}
	var packet []byte
	select {
	case packet = <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("Go peer did not receive authenticated plaintext")
	}
	if len(packet) != 28+len(message) || !bytes.Equal(packet[28:], message) {
		t.Fatal("Go peer received unexpected plaintext")
	}
	copy(packet[12:16], peer.AsSlice())
	copy(packet[16:20], local.AsSlice())
	// Swapping addresses preserves the IPv4 checksum. A zero UDP checksum is valid for IPv4.
	src := binary.BigEndian.Uint16(packet[20:22])
	binary.BigEndian.PutUint16(packet[20:22], 443)
	binary.BigEndian.PutUint16(packet[22:24], src)
	clear(packet[26:28])
	copy(packet[28:], []byte("Go to kernel"))
	if err := port.enqueue(packet, false); err != nil {
		t.Fatal(err)
	}
	if err := sender.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var reply [256]byte
	n, err := sender.Read(reply[:])
	if err != nil || string(reply[:n]) != "Go to kernel" {
		t.Fatalf("kernel did not deliver the Go peer's reply: %v", err)
	}
}

func TestLinuxKernelPeerWithoutNamespacePrivilege(t *testing.T) {
	if os.Getenv("ENDLESSNET_TEST_DENIED_NETNS") != "1" {
		t.Skip("requires a process denied CLONE_NEWNET")
	}
	f := newCandidateFixture(t)
	cfg := directCipherConfig{assignment: f.assignment(t), private: f.profile.Keys.WireGuardPrivate, mtu: 1420, clock: f.clock}
	device, err := openLinuxPeerDevice(t.Context(), cfg)
	if device != nil {
		device.Close()
		t.Fatal("test process acquired an unexpected private device")
	}
	if err != errKernelUnavailable {
		t.Fatalf("want clean kernel unavailability, got %v", err)
	}
	backend := searchTestBackend{productionSearchTestBudget, (*peerCipher)(nil)}
	for _, tc := range []struct {
		name    string
		blockWG bool
	}{{"wireguard", false}, {"amneziawg", true}} {
		t.Run(tc.name, func(t *testing.T) {
			testTwoSupervisorsWithBudget(t, tc.blockWG, false, 0, backend, backend)
		})
	}
}

func TestLinuxKernelSupervisorsSelectWireGuard(t *testing.T) { testLinuxKernelSupervisors(t, false) }

func TestLinuxKernelRetainedControlMeter(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	testRetainedControlMeter(t, func(ctx context.Context, cfg directCipherConfig, c *assignedCipher, p *packetPort) (directPeerEngine, error) {
		engine, err := platformDirectPreparer(ctx, cfg, c, p)
		if err == errKernelUnavailable {
			return nil, fmt.Errorf("required kernel engine %s: %w", cfg.assignment.offer.Profile.Engine, err)
		}
		return engine, err
	})
}
func TestLinuxKernelSupervisorsSelectAmneziaWG(t *testing.T) { testLinuxKernelSupervisors(t, true) }
func testLinuxKernelSupervisors(t *testing.T, filterWG bool) {
	if !linuxNetworkTest(t) {
		return
	}
	makeBudget := func(max int) (*transportBudget, error) {
		resources, err := NewTransportResources(max)
		if err != nil {
			return nil, err
		}
		prepare := resources.budget.kernel
		if prepare == nil {
			t.Fatal("production native preparer missing")
		}
		resources.budget.kernel = func(ctx context.Context, cfg directCipherConfig, c *assignedCipher, p *packetPort) (directPeerEngine, error) {
			engine, err := prepare(ctx, cfg, c, p)
			if err == errKernelUnavailable {
				return nil, ErrEngine // prevent a userspace fallback from passing this test
			}
			if err == nil {
				if _, ok := engine.(*linuxDirectEngine); !ok {
					t.Errorf("unexpected engine %T for %s", engine, cfg.assignment.offer.Profile.Engine)
				}
			}
			return engine, err
		}
		return resources.budget, nil
	}
	backend := searchTestBackend{makeBudget, (*linuxDirectEngine)(nil)}
	testTwoSupervisorsWithBudget(t, filterWG, false, 0, backend, backend)
}

func TestLinuxSupervisorsSelectSameAmneziaProfileAcrossGoAndKernel(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	goBackend := searchTestBackend{newTransportBudget, (*peerCipher)(nil)}
	nativeBackend := searchTestBackend{productionSearchTestBudget, (*linuxDirectEngine)(nil)}
	testTwoSupervisorsWithBudget(t, true, false, 0, goBackend, nativeBackend)
}

func TestLinuxThreeSupervisorsKeepKernelAmneziaPeer(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	testThreeSearchPeers(t, searchTestBackend{newTransportBudget, (*peerCipher)(nil)}, (*peerCipher)(nil), searchTestBackend{productionSearchTestBudget, (*linuxDirectEngine)(nil)}, memoryThreePeerLinks())
}

func TestLinuxHubUsesBothKernelProtocols(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	testThreeSearchPeers(t, searchTestBackend{productionSearchTestBudget, (*linuxDirectEngine)(nil)}, (*linuxDirectEngine)(nil), searchTestBackend{newTransportBudget, (*peerCipher)(nil)}, memoryThreePeerLinks())
}

// Run in a fixture with WireGuard built in or loaded, and no AmneziaWG module.
func TestLinuxHubUsesKernelWGAndGoAWG(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	testThreeSearchPeers(t, searchTestBackend{productionSearchTestBudget, (*linuxDirectEngine)(nil)}, (*peerCipher)(nil), searchTestBackend{newTransportBudget, (*peerCipher)(nil)}, linuxThreePeerUDP(t))
	out, err := exec.CommandContext(t.Context(), "nft", "list", "chain", "ip", "en007test", "out").CombinedOutput()
	if err != nil || len(regexp.MustCompile(`counter packets [1-9][0-9]* bytes`).FindAll(out, -1)) != 2 {
		t.Fatalf("expected packet loss on A-B in both directions: %v: %s", err, out)
	}
	t.Logf("A-B packet counters:\n%s", out)
}

func linuxThreePeerUDP(t *testing.T) threePeerLinks {
	t.Helper()
	run := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
	}
	run("ip", "link", "set", "lo", "up")
	for _, addr := range []string{"192.0.2.10", "192.0.2.20", "192.0.2.30"} {
		run("ip", "addr", "add", addr+"/32", "dev", "lo")
	}
	return threePeerLinks{
		listen: func(addr netip.AddrPort, _ string) (candidateSocket, error) {
			return net.ListenUDP("udp4", net.UDPAddrFromAddrPort(addr))
		},
		disconnectFirst: func() {
			const policy = `add table ip en007test
add chain ip en007test out { type filter hook output priority filter; policy accept; }
add rule ip en007test out ip saddr 192.0.2.20 ip daddr 192.0.2.10 udp sport 51820 counter drop
add rule ip en007test out ip saddr 192.0.2.10 ip daddr 192.0.2.20 udp dport 51820 counter drop
`
			cmd := exec.CommandContext(t.Context(), "nft", "-f", "-")
			cmd.Stdin = strings.NewReader(policy)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("install A-B packet loss: %v: %s", err, out)
			}
		},
	}
}
