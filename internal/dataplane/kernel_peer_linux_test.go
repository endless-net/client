package dataplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
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
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
	"golang.org/x/sys/unix"
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

func TestLinuxSignedKernelPeerPreparation(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	ctx := t.Context()
	f := newCandidateEngineFixture(t, transport.WireGuard)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.LocalProbeIPv4 = "172.20.4.10"
	f.snapshot.Peers[0].Transport.PeerProbeIPv4 = "172.20.4.11"
	f.refresh(t, &cursor)
	a := f.assignment(t)
	id, _ := linuxPolicyFixture()
	prefix, _ := linuxPeerScope(id.owner)
	name := prefix + "00001"
	conn, err := genetlink.Dial(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ep := netip.MustParseAddrPort("127.0.0.1:39102")
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, mtu: 1420, clock: f.clock}
	iface, err := prepareLinuxPeerLink(ctx, conn, cfg, ep, name, id.owner)
	if err != nil {
		t.Fatal(err)
	}
	public, err := peerPublicKey(a.local, a.peer, cfg.private)
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := a.directPeerIPs()
	if err != nil || len(addresses) != 2 {
		t.Fatal("expected application and probe addresses", err)
	}
	family, err := conn.GetFamily(a.offer.Profile.Engine)
	if err != nil {
		t.Fatal(err)
	}
	err = readKernelDevice(conn, family.ID, iface.Index, func(messages []genetlink.Message) error {
		check := func(key []byte, endpoint netip.AddrPort, ips []netip.Addr) error {
			return verifyLinuxPeer(messages, a.local.PublicKey, key, endpoint, ips)
		}
		if err := check(public, ep, []netip.Addr{addresses[1], addresses[0]}); err != nil {
			return err
		}
		if check(public, ep, addresses[:1]) == nil || check(public, ep, []netip.Addr{addresses[0], addresses[0]}) == nil ||
			check(make([]byte, 32), ep, addresses) == nil || check(public, netip.AddrPortFrom(ep.Addr(), 1), addresses) == nil {
			t.Fatal("accepted a different cryptographic route, public key or endpoint")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range addresses {
		route, err := exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "table", "all", "exact", ip.String()+"/32").Output()
		if err != nil || len(route) != 0 {
			t.Fatalf("preparation published host route for %s: %v", ip, err)
		}
	}
	bad := cfg
	bad.private = make([]byte, 32)
	unused := prefix + "00002"
	if _, err := prepareLinuxPeerLink(ctx, conn, bad, ep, unused, id.owner); err == nil {
		t.Fatal("accepted unrelated private key")
	}
	if _, err := net.InterfaceByName(unused); err == nil {
		t.Fatal("invalid configuration left a device behind")
	}
	f.lease.Revoke()
	if err := configureKernelWGPeer(ctx, conn, iface.Index, ep, cfg); err != ErrNoAuthority {
		t.Fatalf("configuration after revocation: %v", err)
	}
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
	f.withRelay(t)
	rt, _, _, _ := relayPipeFixtureFor(t, f)
	mux, err := newRelayMux(t.Context(), rt, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mux.Close)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.LocalProbeIPv4 = "172.20.4.10"
	f.snapshot.Peers[0].Transport.PeerProbeIPv4 = "172.20.4.11"
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts = append(f.snapshot.Peers[0].ACLGrants[0].AllowedPorts, coordinator.ACLPort{Protocol: "udp", Port: 443})
	f.refresh(t, &cursor)
	if err := mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	negotiation := negotiationForFixture(t, mux, f, false)
	qualifyProbeTrials(t, negotiation, f)
	if err := negotiation.Handle(replyControl(*negotiation.commit, transport.Committed)); err != nil {
		t.Fatal(err)
	}
	token, err := negotiation.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	confirmed := negotiation.direct
	a := f.assignment(t)
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "lo", "up")
	id, _ := linuxPolicyFixture()
	prefix, _ := linuxPeerScope(id.owner)
	candidate := prefix + "00001"
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
	reverse.Transport = &coordinator.PeerTransportPlan{LocalProbeIPv4: a.plan.PeerProbeIPv4, PeerProbeIPv4: a.plan.LocalProbeIPv4}
	conn, err := genetlink.Dial(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	outer := netip.MustParseAddrPort("127.0.0.1:39101")
	proxy, err := openLinuxBrokerSocket(ctx, id.mark)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Close() }()
	kernelTarget := proxy.LocalAddr().(*net.UDPAddr).AddrPort()
	iface, err := prepareLinuxPeerLink(ctx, conn, cfg, kernelTarget, candidate, id.owner)
	if err != nil {
		t.Fatal(err)
	}

	policy, err := prepareLinuxFilter(f.snapshot, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := policy.transaction(id, true, linuxDeny, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := linuxNFTApply(ctx, rules); err != nil {
		t.Fatal(err)
	}
	unmarked, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unmarked.Close() }()
	if unexpected, err := startLinuxKernelListener(ctx, conn, iface, id, unmarked); unexpected != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatalf("unmarked broker: guard=%v error=%v", unexpected, err)
	}
	guard, err := startLinuxKernelListener(ctx, conn, iface, id, proxy)
	if err != nil {
		t.Fatal(err)
	}
	kernelPort := guard.port
	if err := guard.retire(ctx); err != ErrAdapterOwnership {
		t.Fatalf("retired guard before driver closed: %v", err)
	}
	driver := netip.AddrPortFrom(kernelTarget.Addr(), kernelPort)
	_, changed := confirmed.lease.SourceSignal()
	broker, err := attachKernelBroker(kernelPeerLink{socket: proxy, driver: driver}, confirmed.bind, a, changed, linuxKernelHandshake(a.offer.Profile))
	if err != nil {
		t.Fatal(err)
	}
	route, err := newPeerRoute(negotiation.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := route.ConfirmRelay(routeRelayProof(t, negotiation, f)); err != nil {
		t.Fatal(err)
	}
	if _, err := route.selectDirect(token, func() error {
		if err := confirmed.bind.checkAdapterLocked(); err != nil {
			return err
		}
		confirmed.bind.adapterProbeOnly = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	negotiation.cycle.cancel()
	reverse.Endpoint = kernelTarget.String()
	handshake := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf [65535]byte
		for {
			n, from, err := proxy.ReadFromUDPAddrPort(buf[:])
			if err != nil {
				return
			}
			switch from {
			case driver:
				if confirmed.bind.handshake(buf[:n]) {
					select {
					case handshake <- struct{}{}:
					default:
					}
				}
				_, err = proxy.WriteToUDPAddrPort(buf[:n], outer)
			case outer:
				err = broker.forwardIngress(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { _ = proxy.Close(); <-done }()
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
	testLinuxServiceExchange(t, f, a, id, candidate, received, port, confirmed, broker)
	select {
	case <-handshake:
	default:
		t.Fatalf("candidate did not recognize a kernel handshake for %s", engine)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "address", "add", local.String()+"/32", "dev", candidate)
	runLinuxTestCommand(t, ctx, linuxIP, "route", "add", peer.String()+"/32", "dev", candidate)
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
	runLinuxTestCommand(t, ctx, linuxIP, "link", "del", candidate)
	if err := guard.retire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := guard.retire(ctx); err != nil {
		t.Fatalf("repeated retirement: %v", err)
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

type linuxRouteClock func() (connectivity.ClockSample, error)

func (sample linuxRouteClock) Sample() (connectivity.ClockSample, error) { return sample() }

func testLinuxServiceExchange(t *testing.T, f *candidateFixture, a candidateAssignment, id linuxFilterIdentity, device string, received <-chan []byte, port *packetPort, confirmed *assignedCipher, broker *kernelCandidateBroker) {
	t.Helper()
	ctx := t.Context()
	sock, err := openLinuxProbeSocket(ctx, id, device, a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sock.conn.Close() }()
	b := sock.binding
	plan, err := prepareLinuxFilter(f.snapshot, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Initial host policy precedes the session-owned probe grant.
	apply := func(install bool, mode linuxFilterMode) {
		t.Helper()
		rules, err := plan.transaction(id, install, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := linuxNFTApply(ctx, rules); err != nil {
			t.Fatal(err)
		}
	}
	apply(broker == nil, linuxOnline)
	if broker != nil {
		// A local UDP socket reaching the wildcard listener must be dropped
		// before WireGuard. Verify the exact input-rule counter in this netns.
		before := linuxFixtureInputDrops(t, id.table, device)
		other, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(broker.link.driver))
		if err != nil {
			t.Fatal(err)
		}
		_, err = other.Write([]byte("fixture"))
		_ = other.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := linuxFixtureInputDrops(t, id.table, device); got != before+1 {
			t.Fatalf("unbrokered delivery did not hit listener guard: %d -> %d", before, got)
		}
	}
	session, err := openLinuxLink(ctx, id.tunnel, 1420)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.device.Close() }()
	if err := session.configure(ctx, plan.address, nil); err != nil {
		t.Fatal(err)
	}
	refreshed, err := prepareLinuxFilter(f.snapshot, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSession := &LinuxSession{link: session, clock: f.clock, lease: f.lease, identity: id, current: f.verified, policy: refreshed,
		receipt: state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	openRelay := func() *RelayTransport {
		relayConn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = relayConn.Close() })
		accepted, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = accepted.Close() })
		_, cancel := context.WithCancel(ctx)
		rt := &RelayTransport{socket: tls.Client(relayConn, &tls.Config{}), ready: true, cancel: cancel, done: make(chan struct{})}
		t.Cleanup(rt.Close)
		return rt
	}
	rt := openRelay()
	if err := runtimeSession.installPeerProbe(ctx, sock, nil); !errors.Is(err, ErrIPConflict) {
		t.Fatalf("missing Relay: %v", err)
	}
	if rt.permitsProbeRoute(b.local.String(), "127.0.0.1") {
		t.Fatal("allowed route over connected Relay")
	}
	runtimeSession.clock = linuxRouteClock(func() (connectivity.ClockSample, error) {
		if len(session.probes) > 0 {
			rt.Close()
		}
		return f.clock.Sample()
	})
	if err := runtimeSession.installPeerProbe(ctx, sock, rt); !errors.Is(err, ErrNoAuthority) || len(session.probes) != 0 {
		t.Fatalf("lost Relay during route installation: err=%v routes=%d", err, len(session.probes))
	}
	runtimeSession.clock = f.clock
	if err := runtimeSession.installPeerProbe(ctx, sock, rt); !errors.Is(err, ErrIPConflict) {
		t.Fatalf("closed Relay: %v", err)
	}
	if err := runtimeSession.installPeerProbe(ctx, sock, openRelay()); err != nil {
		t.Fatal(err)
	}
	if err := runtimeSession.filters(ctx, f.verified, refreshed, false, linuxOnline); err != nil {
		t.Fatal(err)
	}
	if err := session.verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.addProbeRoute(ctx, b); err != ErrIPConflict {
		t.Fatalf("claimed an existing probe address: %v", err)
	}
	req := challengeControl(t, f)
	if _, err := rand.Read(req.Nonce); err != nil {
		t.Fatal(err)
	}
	raw, err := controlPacket(b.local, b.peer, req)
	if err != nil {
		t.Fatal(err)
	}
	// An ordinary process bound to the very same service IP cannot use the
	// probe route. This socket lacks the private mark and reserved local port.
	ordinary, err := net.ListenUDP("udp4", &net.UDPAddr{IP: b.local.AsSlice()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ordinary.Close() }()
	if _, err := ordinary.WriteToUDPAddrPort(raw[28:], netip.AddrPortFrom(b.peer, b.port)); !errors.Is(err, unix.EPERM) {
		t.Fatalf("ordinary process sent service traffic: %v", err)
	}
	if err := sock.send(raw, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case wire := <-received:
		msg, claimed, err := decodeControlPacket(wire, b.local, b.peer)
		if err != nil || !claimed || !bytes.Equal(msg.Nonce, req.Nonce) {
			t.Fatalf("invalid wire challenge: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing probe at Go peer")
	}
	wire, err := controlPacket(b.peer, b.local, replyControl(req, transport.Echo))
	if err != nil {
		t.Fatal(err)
	}
	// An authenticated peer still cannot address the local socket directly:
	// only UDP/0 on the wire is admitted. Supply a valid checksum so the
	// rejection is due to policy, not a malformed UDP packet.
	wrong := bytes.Clone(wire)
	binary.BigEndian.PutUint16(wrong[20:22], b.port)
	binary.BigEndian.PutUint16(wrong[22:24], b.port)
	clear(wrong[26:28])
	sum := packetChecksum(wrong[20:], udpPseudoSum(wrong))
	if sum == 0 {
		sum = 0xffff
	}
	binary.BigEndian.PutUint16(wrong[26:28], sum)
	if err := port.enqueue(wrong, false); err != nil {
		t.Fatal(err)
	}
	if err := sock.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = sock.receive()
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("wire ports admitted ordinary UDP: %v", err)
	}
	if err := port.enqueue(wire, false); err != nil {
		t.Fatal(err)
	}
	if err := sock.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response, err := sock.receive()
	if err != nil || !transport.MatchesReply(req, response) {
		t.Fatalf("kernel path echo: %v", err)
	}
	if a.offer.Profile.Engine == transport.WireGuard {
		testLinuxProbeQuotaOnWire(t, f, confirmed, sock, received, port, broker)
	}
	if err := runtimeSession.filters(ctx, f.verified, refreshed, false, linuxExisting); err != nil {
		t.Fatal(err)
	}
	if err := sock.send(raw, func() error { return nil }); !errors.Is(err, unix.EPERM) {
		t.Fatalf("new offline probe: %v", err)
	}
	if err := session.removeProbeRoute(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := session.verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.addProbeRoute(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := session.device.Close(); err != nil {
		t.Fatal(err)
	}
	route, err := exec.CommandContext(ctx, linuxIP, "route", "show", "table", "all", "exact", b.peer.String()+"/32").Output()
	if err != nil || len(route) != 0 {
		t.Fatalf("service route survived TUN: %v", err)
	}
	// Return the application firewall to the pre-probe state, keeping the
	// driver's independent input guard until its interface is destroyed.
	plan.probes = nil
	_, peerIP, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	plan.direct = map[netip.Addr]string{peerIP: device}
	apply(false, linuxOnline)

}

func testLinuxProbeQuotaOnWire(t *testing.T, f *candidateFixture, channel *assignedCipher, sock *linuxProbeSocket, received <-chan []byte, port *packetPort, broker *kernelCandidateBroker) {
	t.Helper()
	ctx := t.Context()
	// Count WireGuard data on the outer loopback transport. Exclude handshake
	// and empty keepalive datagrams. nft counts IPv4+UDP+encrypted payload.
	err := linuxNFTApply(ctx, `table ip probe_meter {
        counter outgoing {}
        counter incoming {}
        chain observe { type filter hook output priority 0; policy accept;
            oifname "lo" udp dport 39101 @th,64,32 0x04000000 udp length > 40 counter name outgoing
            oifname "lo" udp sport 39101 @th,64,32 0x04000000 udp length > 40 counter name incoming
        }
    }`)
	if err != nil {
		t.Fatal(err)
	}
	readBytes := func(name string) uint64 {
		t.Helper()
		data, err := exec.CommandContext(ctx, linuxNFT, "-j", "list", "counter", "ip", "probe_meter", name).Output()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			NFTables []struct{ Counter *struct{ Bytes uint64 } }
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		for _, entry := range result.NFTables {
			if entry.Counter != nil {
				return entry.Counter.Bytes
			}
		}
		t.Fatal("wire byte counter missing")
		return 0
	}
	cycle, err := newProbeCycle(ctx, f.clock, f.profile.ID, 1, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.cancel()
	probe, err := channel.BeginProbe(cycle, cycle.id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := controlPacket(sock.binding.local, sock.binding.peer, probe.request)
	if err != nil {
		t.Fatal(err)
	}

	if err := sock.sendWireGuard(raw, channel); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("charged datagram not received")
	}
	wire := readBytes("outgoing")
	if wire == 0 || uint64(cycle.wire.consumed()) != wire || uint64(channel.serviceWindow.wire.consumed()) != wire {
		t.Fatalf("budget %d bytes, observed %d", cycle.wire.consumed(), wire)
	}
	// First pass a larger application datagram through the same driver. It
	// raises the inbound size bound without spending the probe allowance.
	local, peer, err := f.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	app := append(cipherIPv4(peer.String(), local.String(), 1), make([]byte, 900)...)
	binary.BigEndian.PutUint16(app[2:4], uint16(len(app)))
	binary.BigEndian.PutUint16(app[24:26], uint16(len(app)-20))
	clear(app[10:12])
	binary.BigEndian.PutUint16(app[10:12], packetChecksum(app[:20], 0))
	if err := port.enqueue(app, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for int(broker.largestIngress.Load()) < len(app) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	beforeEcho := readBytes("incoming")
	if beforeEcho == 0 || cycle.wire.consumed() != int(wire) {
		t.Fatal("application data missing or charged to probe budget")
	}
	reply := replyControl(probe.request, transport.Echo)
	response, err := controlPacket(sock.binding.peer, sock.binding.local, reply)
	if err != nil {
		t.Fatal(err)
	}
	if err := port.enqueue(response, false); err != nil {
		t.Fatal(err)
	}
	if err := sock.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	value, err := sock.receiveWireGuard(channel, broker)
	if err != nil || !transport.MatchesReply(probe.request, value) {
		t.Fatalf("metered echo not accepted: %v", err)
	}
	ingress := int(broker.largestIngress.Load()) + 28
	echoBytes := readBytes("incoming") - beforeEcho
	if echoBytes == 0 || uint64(ingress) <= echoBytes {
		t.Fatalf("expected a conservative debit after a larger packet: echo %d, debit %d", echoBytes, ingress)
	}
	if delta := cycle.wire.consumed() - int(wire); delta != ingress || channel.serviceWindow.wire.consumed() != cycle.wire.consumed() {
		t.Fatalf("ingress debit %d, observed upper bound %d", delta, ingress)
	}
	if err := cycle.charge(transport.MaxProbeBytes - cycle.wire.consumed() - int(wire)); err != nil {
		t.Fatal(err)
	}
	if err := sock.sendWireGuard(raw, channel); err != nil {
		t.Fatalf("packet exactly at limit: %v", err)
	}
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("final admitted datagram missing")
	}
	if observed := readBytes("outgoing"); observed != 2*wire {
		t.Fatalf("expected %d bytes on the wire, got %d", 2*wire, observed)
	}
	t.Logf("WireGuard challenge: %d plaintext IPv4 bytes, %d outer IPv4 bytes; inbound echo %d, charged upper bound %d bytes", len(raw), wire, echoBytes, ingress)
	if err := port.enqueue(response, false); err != nil {
		t.Fatal(err)
	}
	if _, err := sock.receiveWireGuard(channel, broker); !errors.Is(err, ErrProbeBudget) {
		t.Fatalf("accepted inbound echo after exhaustion: %v", err)
	}
	if err := sock.sendWireGuard(raw, channel); !errors.Is(err, ErrProbeBudget) {
		t.Fatalf("exhausted probe budget: %v", err)
	}
	if next := readBytes("outgoing"); next != 2*wire {
		t.Fatalf("sent %d more bytes after exhaustion", next-2*wire)
	}
	if cycle.wire.consumed() != transport.MaxProbeBytes {
		t.Fatal("failed admission changed the budget")
	}
}

func linuxFixtureInputDrops(t *testing.T, table, chainName string) uint64 {
	t.Helper()
	data, err := exec.CommandContext(t.Context(), linuxNFT, "-j", "list", "chain", "inet", table, chainName).Output()
	if err != nil {
		t.Fatal(err)
	}
	var chain struct {
		NFTables []struct {
			Rule *struct {
				Expr []struct {
					Counter *struct{ Packets uint64 }
					Drop    json.RawMessage
				}
			}
		}
	}
	if err := json.Unmarshal(data, &chain); err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, entry := range chain.NFTables {
		if entry.Rule == nil {
			continue
		}
		var count uint64
		for _, expr := range entry.Rule.Expr {
			if expr.Counter != nil {
				count = expr.Counter.Packets
			}
			if expr.Drop != nil {
				total += count
			}
		}
	}
	return total
}

func TestLinuxBindingFollowsSignedPairAcrossRefresh(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.LocalProbeIPv4 = "172.20.4.10"
	f.snapshot.Peers[0].Transport.PeerProbeIPv4 = "172.20.4.11"
	f.refresh(t, &cursor)
	a := f.assignment(t)
	local, peer, err := a.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	_, application, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	epoch, _ := f.lease.SourceSignal()
	b := linuxProbeBinding{assignment: &a, sourceEpoch: epoch, local: local, peer: peer, port: 30001, device: "ep0000000000001"}
	link := linuxLink{probes: map[netip.Addr]linuxProbeBinding{peer: b}, direct: map[netip.Addr]net.Interface{application: {Name: b.device}}}
	check := func(granted bool) {
		t.Helper()
		plan := linuxFilterPlan{}
		snap, err := f.verified.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		now, err := f.clock.Sample()
		if err != nil {
			t.Fatal(err)
		}
		retained := link.applyPeerBindings(&plan, nil, f.verified, snap, f.lease, now.Wall)
		if (len(plan.probes) == 1) != granted || (plan.direct[application] == b.device) != granted || len(retained) != 2 || retained[0] != local || retained[1] != peer {
			t.Fatalf("granted=%v probe count=%d direct=%q retained=%v", granted, len(plan.probes), plan.direct[application], retained)
		}
	}
	cursor = f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	check(true)
	cursor = f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport = nil
	f.refresh(t, &cursor)
	check(false)
}

func TestLinuxSupervisorPreparesHostCandidate(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	ctx := t.Context()
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "lo", "up")
	f := newCandidateEngineFixture(t, transport.WireGuard)
	f.withRelay(t)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.LocalProbeIPv4 = "172.20.4.10"
	f.snapshot.Peers[0].Transport.PeerProbeIPv4 = "172.20.4.11"
	f.refresh(t, &cursor)
	rt, _, _, _ := relayPipeFixtureFor(t, f)
	mux, err := newRelayMux(ctx, rt, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer mux.Close()
	bind, err := mux.newBind(ctx, f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	defer bind.shutdown()
	id, _ := linuxPolicyFixture()
	policy, err := prepareLinuxFilter(f.snapshot, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	session := &LinuxSession{lease: f.lease, clock: f.clock, identity: id, receipt: state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}, policy: policy, current: f.verified}
	session.ctx, session.cancel = context.WithCancel(ctx)
	defer session.cancel()
	if err := session.filters(ctx, f.verified, policy, true, linuxDeny); err != nil {
		t.Fatal(err)
	}
	session.link, err = openLinuxLink(ctx, id.tunnel, 1420)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.link.device.Close() }()
	if err := session.link.configure(ctx, policy.address, policy.protected[1:]); err != nil {
		t.Fatal(err)
	}
	g := &transportSupervisor{budget: &transportBudget{kernel: platformDirectPreparer}, config: supervisorConfig{hostKernel: session.prepareHostCandidate}, snapshot: f.snapshot}
	peer := &supervisedRelay{channel: &relayCipher{bind: bind}, scope: packetBinding{peerID: "peer"}}
	a := f.assignment(t)
	openUnderlay := func(endpoint string) *net.UDPConn {
		address := netip.MustParseAddrPort(endpoint)
		runLinuxTestCommand(t, ctx, linuxIP, "addr", "add", address.Addr().String()+"/32", "dev", "lo")
		socket, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(address))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = socket.Close() })
		return socket
	}
	external := openUnderlay(a.localEndpoint)
	remoteSocket := openUnderlay(a.peerEndpoint)
	remote := reverseSearchFixture(t, f)
	other := remote.snapshot.Peers[0]
	remoteChannel, err := prepareAssignedCipher(ctx, remote.verified, remote.lease, other.ID, a.offer.Profile.ID, remote.profile.Keys.WireGuardPrivate, 1420, remote.clock, func([]byte) error { return nil }, func(uint16) (candidateSocket, error) { return remoteSocket, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer remoteChannel.Close()

	channel, err := prepareAssignedCipher(ctx, f.verified, f.lease, a.peer.ID, a.offer.Profile.ID, f.profile.Keys.WireGuardPrivate, 1420, f.clock, func([]byte) error { return nil }, func(uint16) (candidateSocket, error) { return external, nil }, g.kernelForPeer(peer))
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	engine, ok := channel.cipher.(*linuxHostEngine)
	if !ok {
		t.Fatalf("unexpected engine %T", channel.cipher)
	}
	device, err := net.InterfaceByIndex(engine.iface.Index)
	if err != nil || device.Flags&net.FlagUp != 0 {
		t.Fatal("prepared interface must be down", err)
	}
	if _, err := engine.BoundPort(); err != nil {
		t.Fatal(err)
	}
	const candidate = "66778899001122334455667788990011"
	cycle, err := newProbeCycle(ctx, f.clock, f.profile.ID, f.profile.Connection.Generation, candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.cancel()
	probe, err := channel.BeginProbe(cycle, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.link.probes) != 1 || len(session.link.direct) != 0 {
		t.Fatal("expected a service route only")
	}
	otherCycle, err := newProbeCycle(ctx, remote.clock, remote.profile.ID, remote.profile.Connection.Generation, candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer otherCycle.cancel()
	if _, err := remoteChannel.BeginProbe(otherCycle, candidate); err != nil {
		t.Fatal(err)
	}
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.done:
	case <-time.After(3 * time.Second):
		t.Fatal("missing authenticated echo from software peer")
	}
	if _, err := probe.Evidence(); err != nil {
		t.Fatal(err)
	}
	channel.Close()
	if len(session.link.probes) != 0 || f.lease.Permissions().Revoked {
		t.Fatal("candidate retirement failed")
	}
	if _, err := net.InterfaceByIndex(engine.iface.Index); err == nil {
		t.Fatal("orphaned candidate")
	}
}

func TestLinuxSupervisorsSelectHostRoute(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	native := searchTestBackend{productionSearchTestBudget, (*linuxHostEngine)(nil)}
	software := searchTestBackend{newTransportBudget, (*peerCipher)(nil)}
	testTwoSupervisorsThroughSelection(t, false, false, 0, native, software, false, linuxSearchRouteFixture, false)
}
func TestLinuxSupervisorsKeepIndependentProfiles(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	native := searchTestBackend{productionSearchTestBudget, (*linuxHostEngine)(nil)}
	software := searchTestBackend{newTransportBudget, (*peerCipher)(nil)}
	links := memoryThreePeerLinks()
	links.setupHub = linuxSearchRouteFixture
	testThreeSearchPeers(t, native, (*peerCipher)(nil), software, links)
}

func linuxSearchRouteFixture(t *testing.T, f *candidateFixture, config *supervisorConfig) searchRouteFixture {
	ctx := t.Context()
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "lo", "up")
	id, _ := linuxPolicyFixture()
	s := &LinuxSession{identity: id, receipt: state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}, lease: f.lease, clock: f.clock, current: f.verified}
	s.ctx, s.cancel = context.WithCancel(ctx)
	t.Cleanup(s.cancel)
	var err error
	s.policy, err = prepareLinuxFilter(f.snapshot, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.filters(ctx, s.current, s.policy, true, linuxDeny); err != nil {
		t.Fatal(err)
	}
	s.link, err = openLinuxLink(ctx, id.tunnel, 1420)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.link.device.Close() })
	if err := s.link.configure(ctx, s.policy.address, s.policy.protected[1:]); err != nil {
		t.Fatal(err)
	}
	if err := s.filters(ctx, s.current, s.policy, false, linuxOnline); err != nil {
		t.Fatal(err)
	}
	config.hostKernel = s.prepareHostCandidate
	config.selectDirect = s.selectHostDirect
	return searchRouteFixture{refresh: func(m connectivity.Map) {
		s.mu.Lock()
		defer s.mu.Unlock()
		snapshot, err := m.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		updated, err := prepareLinuxFilter(snapshot, 100, 100)
		if err != nil {
			t.Fatal(err)
		}
		s.retained = append(s.policy.protected[:0:0], s.policy.protected...)
		if err := s.filters(ctx, m, updated, false, linuxOnline); err != nil {
			t.Fatal(err)
		}
		if err := s.link.configure(ctx, updated.address, updated.protected[1:]); err != nil {
			t.Fatal(err)
		}
		s.current, s.policy = m, updated
		if err := s.filters(ctx, m, s.policy, false, linuxOnline); err != nil {
			t.Fatal(err)
		}
	}, verifyFallback: func(app *net.UDPConn) {
		if _, err := app.Write([]byte("fallback-primary")); err != nil {
			t.Fatal(err)
		}
		read := make(chan bool, 1)
		go func() {
			buf := make([]byte, 65535+2*tun.ReadPacketSpacing)
			packets := make([]tun.ReadPacket, 1)
			for {
				n, err := s.link.device.Read(buf, packets)
				if err != nil {
					read <- false
					return
				}
				if n == 1 && packets[0].Size >= 28 && bytes.Equal(buf[packets[0].Offset+28:packets[0].Offset+packets[0].Size], []byte("fallback-primary")) {
					read <- true
					return
				}
			}
		}()
		select {
		case ok := <-read:
			if !ok {
				t.Fatal("primary interface closed")
			}
		case <-time.After(time.Second):
			t.Fatal("fallback payload missing from primary interface")
		}
	}}
}
