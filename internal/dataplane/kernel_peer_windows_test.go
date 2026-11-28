package dataplane

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/wintundriver"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
)

func TestWindowsProbeRoutesPreserveConnectedRelay(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	bind, err := mux.newBind(t.Context(), f.verified, f.lease, "peer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bind.shutdown)
	p := &supervisedRelay{channel: &relayCipher{bind: bind}, scope: packetBinding{peerID: "peer"}}
	g := &transportSupervisor{snapshot: f.snapshot, budget: &transportBudget{kernel: prepareWindowsDirect}}
	plan := g.peerPlan(p)
	for _, tc := range []struct {
		local, peer string
		wantKernel  bool
	}{
		{"192.0.2.96", "10.1.1.2", false},
		{"10.1.1.2", "192.0.2.96", false},
		{"10.1.1.2", "10.1.1.3", true},
	} {
		plan.LocalProbeIPv4, plan.PeerProbeIPv4 = tc.local, tc.peer
		if got := g.kernelForPeer(p) != nil; got != tc.wantKernel {
			t.Fatalf("probe pair %s/%s enabled kernel=%v", tc.local, tc.peer, got)
		}
	}
	p.channel = &relayCipher{bind: &relayPeerBind{mux: &relayMux{transport: &RelayTransport{}}}}
	if g.kernelForPeer(p) != nil {
		t.Fatal("missing Relay socket allowed installation of probe routes")
	}
}

func TestWindowsConfirmedProbeIngressUsesLinkUpperBound(t *testing.T) {
	channel, _, probe := confirmedMeterFixture(t)
	b := channel.bind
	b.mu.Lock()
	b.adapterSocket = b.socket
	broker := &kernelCandidateBroker{bind: b, external: b.socket}
	b.mu.Unlock()
	echo := meterRaw(t, channel, replyControl(probe.request, transport.Echo), true)
	account := func(window *probeCycle) error { return broker.chargeIngress(channel, window) }
	for index, size := range []int32{592, 624} {
		broker.largestIngress.Store(size)
		if err := channel.receiveControl(echo, account); err != nil {
			t.Fatal(err)
		}
		want := []int{620, 1272}[index]
		if got := probe.attempt.cycle.wire.consumed(); got != want {
			t.Fatalf("after datagram %d charged %d, want %d", index+1, got, want)
		}
	}
	window := channel.serviceWindow
	if err := window.charge(transport.MaxProbeBytes - window.wire.consumed() - 651); err != nil {
		t.Fatal(err)
	}
	if err := channel.receiveControl(echo, account); !errors.Is(err, ErrProbeBudget) {
		t.Fatalf("accepted an echo with insufficient remaining budget: %v", err)
	}
	if got := probe.attempt.cycle.wire.consumed(); got != 1272 {
		t.Fatalf("rejected echo consumed search allowance: %d", got)
	}
}

func TestWindowsProbeCiphertextSize(t *testing.T) {
	for _, test := range []struct{ inner, mtu, want int }{
		{576, 1420, 608},
		{577, 1420, 624},
		{577, 581, 613},
		{581, 581, 613},
		{582, 581, 0},
		{27, 1420, 0},
	} {
		got, err := wireGuardProbeCiphertextSize(test.inner, test.mtu)
		if got != test.want || (err != nil) != (test.want == 0) {
			t.Fatalf("inner=%d mtu=%d: size=%d err=%v", test.inner, test.mtu, got, err)
		}
	}
}

func TestWindowsPeerSocketOwnsEndpointAndFiltersDriverSource(t *testing.T) {
	open := func() *net.UDPConn {
		t.Helper()
		s, err := openWindowsPeerSocket(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		if err := s.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	broker, driverSocket, unrelated := open(), open(), open()
	serviceEndpoint := broker.LocalAddr().(*net.UDPAddr).AddrPort()
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var optErr error
		err := c.Control(func(fd uintptr) {
			optErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
		})
		return errors.Join(err, optErr)
	}}
	other, err := lc.ListenPacket(t.Context(), "udp4", serviceEndpoint.String())
	if err == nil {
		other.Close()
		t.Fatal("reused an owned candidate socket")
	}
	if !errors.Is(err, windows.WSAEACCES) && !errors.Is(err, windows.WSAEADDRINUSE) {
		t.Fatalf("unexpected bind failure: %v", err)
	}
	link := kernelPeerLink{broker, driverSocket.LocalAddr().(*net.UDPAddr).AddrPort()}
	if _, err := unrelated.WriteToUDPAddrPort([]byte("unrelated"), serviceEndpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := driverSocket.WriteToUDPAddrPort([]byte("from driver"), serviceEndpoint); err != nil {
		t.Fatal(err)
	}
	var raw [64]byte
	n, err := link.read(raw[:])
	if err != nil || string(raw[:n]) != "from driver" {
		t.Fatalf("unexpected private receive %q: %v", raw[:n], err)
	}
	if err := link.write([]byte("to driver")); err != nil {
		t.Fatal(err)
	}
	n, source, err := driverSocket.ReadFromUDPAddrPort(raw[:])
	if err != nil || source != serviceEndpoint || string(raw[:n]) != "to driver" {
		t.Fatalf("unexpected private send from %v: %v", source, err)
	}
}

func TestWindowsDirectWithoutBundledWireGuardNT(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "wireguard.dll")); err == nil {
		t.Skip("run the test executable without a bundled SDK")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := wintundriver.LoadWireGuardNT(); !errors.Is(err, wintundriver.ErrDriver) {
		t.Fatalf("expected the pinned loader to reject the missing SDK: %v", err)
	}
	f := newCandidateEngineFixture(t, transport.WireGuard)
	a := f.assignment(t)
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	peer, err := openWindowsPeerDevice(t.Context(), cfg)
	if peer != nil {
		defer peer.Close()
	}
	if err != errKernelUnavailable || peer != nil {
		t.Fatalf("unexpected kernel preparation with absent SDK: %v", err)
	}
	r, err := NewTransportResources(1)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := r.budget.newAssigned(t.Context(), f.verified, f.lease, a.peer.ID, a.offer.Profile.ID, cfg.private, 1420, f.clock, func([]byte) error { return nil }, func(uint16) (candidateSocket, error) { return newCandidateSocketFixture(), nil }, r.budget.kernel)
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	goPeer, ok := channel.cipher.(*peerCipher)
	if !ok || goPeer.profileDigest != a.offer.Digest {
		t.Fatal("client did not use the Go engine with the signed WG profile")
	}
	backend := searchTestBackend{productionSearchTestBudget, (*peerCipher)(nil)}
	testTwoSupervisorsWithBudget(t, false, false, 0, backend, backend)
}

type kernelConfigFixture struct {
	state driver.AdapterState
	set   func(*driver.Interface, uint32) error
}

func TestWindowsPeerActivationRequiresOriginalSource(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	a := f.assignment(t)
	_, changed := a.lease.SourceSignal()
	socket, err := openWindowsPeerSocket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	d := windowsPeerDevice{Adapter: &driver.Adapter{}, socket: socket, guard: &nativeFilterAPI{}, assignment: a, sourceChanged: changed}
	epoch := a.lease.BlockSource()
	if err := a.lease.EndSourceTransition(epoch); err != nil {
		t.Fatal(err)
	}
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	if err := d.activate(t.Context(), cfg); !errors.Is(err, ErrNoAuthority) {
		t.Fatalf("activation across source change: %v", err)
	}
}

func (f kernelConfigFixture) AdapterState() (driver.AdapterState, error) { return f.state, nil }
func (f kernelConfigFixture) SetConfiguration(cfg *driver.Interface, size uint32) error {
	return f.set(cfg, size)
}

func TestWindowsKernelConfigUsesExactPairAndPrivateSocket(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	a := f.assignment(t)
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	socket := netip.MustParseAddrPort("127.0.0.1:31723")
	wantKey, err := peerPublicKey(a.local, a.peer, cfg.private)
	if err != nil {
		t.Fatal(err)
	}
	_, ip, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	var sent *driver.Interface
	strict := kernelConfigFixture{set: func(c *driver.Interface, size uint32) error {
		sent = c
		if c.PeerCount != 1 || c.Flags != driver.InterfaceHasPrivateKey|driver.InterfaceHasListenPort|driver.InterfaceReplacePeers || c.ListenPort != 0 || !bytes.Equal(c.PrivateKey[:], cfg.private) {
			t.Fatal("invalid interface configuration")
		}
		p := c.FirstPeer()
		wantFlags := driver.PeerHasPublicKey | driver.PeerHasEndpoint | driver.PeerReplaceAllowedIPs | driver.PeerHasPersistentKeepalive
		if size != uint32(unsafe.Sizeof(driver.Interface{})+unsafe.Sizeof(driver.Peer{})+unsafe.Sizeof(driver.AllowedIP{})) || p.Flags != wantFlags || p.Endpoint.AddrPort() != socket || p.PersistentKeepalive != 0 || !bytes.Equal(p.PublicKey[:], wantKey) || p.AllowedIPsCount != 1 {
			t.Fatal("unexpected peer configuration")
		}
		allowed := p.FirstAllowedIP()
		if allowed.AddressFamily != windows.AF_INET || allowed.Cidr != 32 || !bytes.Equal(allowed.Address[:4], ip.AsSlice()) {
			t.Fatal("unexpected allowed address")
		}
		return nil
	}}
	if err := configureWindowsKernelPeer(t.Context(), strict, socket, cfg); err != nil {
		t.Fatal(err)
	}
	if sent == nil || sent.PrivateKey != [32]byte{} {
		t.Fatal("configuration buffer retained private key")
	}
	strict.set = func(*driver.Interface, uint32) error { t.Fatal("unexpected configuration write"); return nil }
	strict.state = driver.AdapterStateUp
	if !errors.Is(configureWindowsKernelPeer(t.Context(), strict, socket, cfg), ErrAdapterConflict) {
		t.Fatal("accepted active adapter")
	}
	strict.state = driver.AdapterStateDown
	if !errors.Is(configureWindowsKernelPeer(t.Context(), strict, netip.MustParseAddrPort(a.peerEndpoint), cfg), ErrConfiguration) {
		t.Fatal("accepted external socket")
	}
	f.lease.Revoke()
	if !errors.Is(configureWindowsKernelPeer(t.Context(), strict, socket, cfg), ErrNoAuthority) {
		t.Fatal("accepted revoked lease")
	}
}

func TestWindowsKernelConfigRejectsAWGAndRechecksAuthorityAfterSet(t *testing.T) {
	f := newCandidateFixture(t)
	cfg := directCipherConfig{assignment: f.assignment(t), private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	socket := netip.MustParseAddrPort("127.0.0.1:23700")
	if !errors.Is(configureWindowsKernelPeer(t.Context(), kernelConfigFixture{}, socket, cfg), errKernelUnavailable) {
		t.Fatal("WireGuardNT accepted an AmneziaWG profile")
	}
	f = newCandidateEngineFixture(t, transport.WireGuard)
	cfg = directCipherConfig{assignment: f.assignment(t), private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	changing := kernelConfigFixture{set: func(*driver.Interface, uint32) error {
		epoch := f.lease.BlockSource()
		return f.lease.EndSourceTransition(epoch)
	}}
	if !errors.Is(configureWindowsKernelPeer(t.Context(), changing, socket, cfg), ErrNoAuthority) {
		t.Fatal("accepted obsolete source")
	}
}

type windowsReadbackFixture struct{ actual *driver.Interface }

func (f windowsReadbackFixture) Configuration() (*driver.Interface, error) { return f.actual, nil }

func TestWindowsKernelPeerRejectsUnexpectedReadback(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	cfg := directCipherConfig{assignment: f.assignment(t), private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	socket := netip.MustParseAddrPort("127.0.0.1:31723")
	for _, tc := range []struct {
		name   string
		change func(*driver.Interface)
	}{
		{"same pair", nil},
		{"extra peer", func(c *driver.Interface) { c.PeerCount = 2 }},
		{"wrong local key", func(c *driver.Interface) { c.PublicKey[0] ^= 1 }},
		{"wrong private key", func(c *driver.Interface) { c.PrivateKey[1] ^= 1 }},
		{"wrong peer key", func(c *driver.Interface) { c.FirstPeer().PublicKey[0] ^= 1 }},
		{"wrong endpoint", func(c *driver.Interface) {
			_ = c.FirstPeer().Endpoint.SetAddrPort(netip.MustParseAddrPort("127.0.0.1:31724"))
		}},
		{"wider route", func(c *driver.Interface) { c.FirstPeer().FirstAllowedIP().Cidr = 24 }},
		{"wrong host", func(c *driver.Interface) { c.FirstPeer().FirstAllowedIP().Address[3] ^= 1 }},
		{"unexpected preshared key", func(c *driver.Interface) { c.FirstPeer().PresharedKey[0] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var builder driver.ConfigBuilder
			var iface driver.Interface
			iface.PeerCount = 1
			iface.Flags = driver.InterfaceHasPublicKey | driver.InterfaceHasPrivateKey
			copy(iface.PrivateKey[:], cfg.private)
			key, err := base64.StdEncoding.DecodeString(cfg.assignment.local.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			copy(iface.PublicKey[:], key)
			builder.AppendInterface(&iface)
			p := driver.Peer{AllowedIPsCount: 1, Flags: driver.PeerHasEndpoint | driver.PeerHasPublicKey}
			key, err = peerPublicKey(cfg.assignment.local, cfg.assignment.peer, cfg.private)
			if err != nil {
				t.Fatal(err)
			}
			copy(p.PublicKey[:], key)
			if err := p.Endpoint.SetAddrPort(socket); err != nil {
				t.Fatal(err)
			}
			builder.AppendPeer(&p)
			allowed := driver.AllowedIP{AddressFamily: windows.AF_INET, Cidr: 32}
			_, remote, err := cfg.assignment.addresses()
			if err != nil {
				t.Fatal(err)
			}
			copy(allowed.Address[:], remote.AsSlice())
			builder.AppendAllowedIP(&allowed)
			actual, _ := builder.Interface()
			if tc.change != nil {
				tc.change(actual)
			}
			err = verifyWindowsKernelPeer(windowsReadbackFixture{actual}, socket, cfg)
			if (err == nil) != (tc.change == nil) {
				t.Fatalf("unexpected readback result %v", err)
			}
			if actual.PrivateKey != [32]byte{} {
				t.Fatal("readback retained private key")
			}
		})
	}
}
