//go:build windows && integration

package dataplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/wintundriver"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/driver"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// Run in an elevated Windows fixture with the pinned DLL next to the test
// executable. The SDK may install its driver when creating the first adapter.
func TestWindowsOwnedWireGuardNTPeer(t *testing.T) {
	if os.Getenv("ENDLESSNET_TEST_WINDOWS_KERNEL_ADAPTER") != "1" {
		t.Skip("requires an isolated Windows fixture for adapter creation")
	}
	t.Run("driver UDP policy", testWindowsDriverUDPPolicy)
	t.Run("signed probe route", testWindowsSignedProbeRoute)
	t.Run("assigned candidate lifetime", testWindowsAssignedCandidateLifetime)
	t.Run("assigned candidate echo", func(t *testing.T) { testWindowsExchangeFixture(t, "assigned") })
	native := searchTestBackend{productionSearchTestBudget, (*windowsDirectEngine)(nil)}
	goBackend := searchTestBackend{func(maxPeers int) (*transportBudget, error) {
		b, err := newTransportBudget(maxPeers)
		if err == nil {
			b.kernel = nil
		}
		return b, err
	}, (*peerCipher)(nil)}
	t.Run("supervisor route rejection", func(t *testing.T) {
		testTwoSupervisorsThroughSelection(t, false, false, 0, native, goBackend, true, nil, false)
	})
	t.Run("supervisor route selection", func(t *testing.T) {
		testTwoSupervisorsThroughSelection(t, false, false, 0, native, goBackend, false, windowsSearchRouteFixture, false)
	})
	t.Run("independent peer routes", func(t *testing.T) {
		links := memoryThreePeerLinks()
		links.setupHub = windowsSearchRouteFixture
		testThreeSearchPeers(t, native, (*peerCipher)(nil), goBackend, links)
	})
	t.Run("revocation", func(t *testing.T) { testWindowsExchangeFixture(t, "") })
	t.Run("source lifetime", func(t *testing.T) { testWindowsExchangeFixture(t, "source") })
	t.Run("routed application source", func(t *testing.T) { testWindowsExchangeFixture(t, "routed") })
	t.Run("online maintenance policy", func(t *testing.T) { testWindowsExchangeFixture(t, "maintenance") })
	for _, direction := range []string{"outgoing", "incoming"} {
		t.Run("probe budget "+direction, func(t *testing.T) { testWindowsExchangeFixture(t, direction) })
	}
}

func testWindowsExchangeFixture(t *testing.T, scenario string) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	if scenario == "maintenance" || scenario == "source" || scenario == "routed" {
		grant := &f.snapshot.Peers[0].ACLGrants[0]
		grant.AllowedPorts = append(grant.AllowedPorts, coordinator.ACLPort{Protocol: "udp", Port: 443})
	}
	// Both test endpoints are real sockets on the isolated guest's underlay.
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var underlay netip.Addr
	for _, addr := range addrs {
		prefix, e := netip.ParsePrefix(addr.String())
		if e == nil && prefix.Addr().Is4() && prefix.Addr().IsGlobalUnicast() {
			underlay = prefix.Addr()
			break
		}
	}
	if !underlay.IsValid() {
		t.Fatal("guest has no IPv4 underlay")
	}
	open := func() *net.UDPConn {
		s, e := openExclusiveUDP(t.Context(), "udp4", netip.AddrPortFrom(underlay, 0).String())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	external, remoteSocket := open(), open()
	localAddr, remoteAddr := external.LocalAddr().String(), remoteSocket.LocalAddr().String()
	plan := f.snapshot.Peers[0].Transport
	plan.LocalProbeIPv4, plan.PeerProbeIPv4 = "172.20.4.10", "172.20.4.11"
	f.snapshot.Node.Transport.Endpoints[0].Endpoint = localAddr
	plan.PeerAdvertisement.Endpoints[0].Endpoint = remoteAddr
	plan.Profiles[0].LocalEndpoints, plan.Profiles[0].PeerEndpoints = []string{localAddr}, []string{remoteAddr}
	cursor := f.verified.Cursor()
	f.snapshot.Network.Revision++
	f.snapshot.Revision.Network++
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if scenario == "assigned" {
		testWindowsAssignedEcho(t, f, external, remoteSocket)
		return
	}
	a := f.assignment(t)
	cfg := directCipherConfig{assignment: a, private: f.profile.Keys.WireGuardPrivate, clock: f.clock}
	adapter, err := openWindowsPeerDevice(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Error(err)
		}
	}()
	socket := adapter.socket.LocalAddr().(*net.UDPAddr).AddrPort()
	s, err := adapter.AdapterState()
	if err != nil || s != driver.AdapterStateDown {
		t.Fatalf("expected down adapter, state=%v err=%v", s, err)
	}
	actual, err := adapter.Configuration()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(actual.PrivateKey[:])
	if actual.PeerCount != 1 {
		t.Fatalf("expected one peer, got %d", actual.PeerCount)
	}
	p := actual.FirstPeer()
	public, err := peerPublicKey(a.local, a.peer, cfg.private)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.PublicKey[:], public) || p.Endpoint.AddrPort() != socket || p.AllowedIPsCount != 2 {
		t.Fatal("driver returned different peer settings")
	}
	if err := verifyWindowsKernelPeer(adapter, socket, cfg); err != nil {
		t.Fatal(err)
	}

	testWindowsPeerExchange(t, f, adapter, cfg, external, remoteSocket, challengeControl(t, f), scenario)
}

func testWindowsAssignedEcho(t *testing.T, local *candidateFixture, external, remoteSocket *net.UDPConn) {
	t.Helper()
	remote := reverseSearchFixture(t, local)
	const candidateID = "66778899001122334455667788990011"
	var probes [2]*pathChallenge
	var channels [2]*assignedCipher
	observed := &windowsObservedSocket{UDPConn: external}
	fixtures := [2]*candidateFixture{remote, local}
	for i, f := range fixtures {
		var socket candidateSocket = remoteSocket
		var prepare kernelPreparer
		if i == 1 {
			socket, prepare = observed, prepareWindowsDirect
		}
		c, err := prepareAssignedCipher(t.Context(), f.verified, f.lease, f.snapshot.Peers[0].ID,
			f.snapshot.Peers[0].Transport.Profiles[0].Profile.ID, f.profile.Keys.WireGuardPrivate,
			1420, f.clock, func([]byte) error { return ErrNoAuthority },
			func(uint16) (candidateSocket, error) { return socket, nil }, prepare)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		channels[i] = c
		if i == 1 {
			if _, ok := c.cipher.(*windowsDirectEngine); !ok {
				t.Fatal("assigned candidate did not choose WireGuardNT")
			}
		}
	}
	// Both listeners must exist before starting the Go peer's three-second
	// attempt. Bringing up the Windows interface can take longer under load.
	for _, i := range []int{1, 0} {
		f := fixtures[i]
		cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, f.profile.Connection.Generation, candidateID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cycle.cancel)
		probes[i], err = channels[i].BeginProbe(cycle, candidateID)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []int{1, 0} {
		p := probes[index]
		if err := p.Send(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Fatalf("assigned candidate %d did not receive its own authenticated echo", index)
		}
		if _, err := p.Evidence(); err != nil {
			t.Fatal(err)
		}
		if index == 1 {
			// Model the local receive grant while the bilateral commit is
			// pending. The remote still needs its own authenticated echo.
			bind := channels[1].bind
			bind.mu.Lock()
			bind.receiveConfirmed = true
			bind.confirmationEpoch++
			bind.mu.Unlock()
		}
	}
	used := probes[1].attempt.cycle.wire.consumed()
	if got := observed.inBytes.Load() + observed.outBytes.Load(); used == 0 || uint64(used) != got {
		t.Fatalf("pending route commit charged %d bytes, observed %d on public socket", used, got)
	}
}

func testWindowsAssignedCandidateLifetime(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	plan := f.snapshot.Peers[0].Transport
	plan.LocalProbeIPv4, plan.PeerProbeIPv4 = "172.20.4.10", "172.20.4.11"
	f.refresh(t, &cursor)
	a := f.assignment(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c, err := prepareAssignedCipher(ctx, f.verified, f.lease, a.peer.ID, a.offer.Profile.ID,
		f.profile.Keys.WireGuardPrivate, 1420, f.clock, func([]byte) error { return ErrNoAuthority }, nil, prepareWindowsDirect)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	engine, ok := c.cipher.(*windowsDirectEngine)
	if !ok {
		t.Fatal("unexpected engine", c.cipher)
	}
	port, err := engine.BoundPort()
	if err != nil || port == 0 {
		t.Fatal("missing candidate port", err)
	}
	luid := engine.device.LUID()
	local, _, err := a.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := luid.IPAddress(local); err != nil {
		t.Fatal("missing candidate probe address", err)
	}
	if err := engine.Queue([]byte{1}, false); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("unqualified adapter accepted application traffic", err)
	}
	if state, err := engine.device.AdapterState(); err != nil || state != driver.AdapterStateDown {
		t.Fatal("advertised adapter was active before an acknowledged probe", err)
	}
	f.lease.BlockSource()
	if _, err := engine.BoundPort(); !errors.Is(err, ErrNoAuthority) {
		t.Fatal("prepared adapter still advertised its port after source replacement", err)
	}
	cancel()
	select {
	case <-c.done:
	case <-time.After(8 * time.Second):
		t.Fatal("assigned candidate failed to stop")
	}
	if _, err := luid.IPAddress(local); err == nil {
		t.Fatal("candidate retained its probe address")
	}
	again, err := openExclusiveUDP(t.Context(), "udp4", netip.AddrPortFrom(netip.IPv4Unspecified(), port).String())
	if err != nil {
		t.Fatal("candidate retained its public port", err)
	}
	again.Close()
}

func testWindowsSignedProbeRoute(t *testing.T) {
	f := newCandidateEngineFixture(t, transport.WireGuard)
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.LocalProbeIPv4 = "172.20.4.10"
	f.snapshot.Peers[0].Transport.PeerProbeIPv4 = "172.20.4.11"
	f.refresh(t, &cursor)
	cfg := directCipherConfig{assignment: f.assignment(t), private: f.profile.Keys.WireGuardPrivate, mtu: 1420, clock: f.clock}
	device, err := openWindowsPeerDevice(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := device.Close(); err != nil {
			t.Error(err)
		}
	})
	local, remote, err := cfg.assignment.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	luid := device.LUID()
	address, err := luid.IPAddress(local)
	if err != nil || !address.SkipAsSource || address.OnLinkPrefixLength != 32 {
		t.Fatal("missing dedicated probe address", err)
	}
	if _, err := luid.Route(netip.PrefixFrom(remote, 32), netip.IPv4Unspecified()); err != nil {
		t.Fatal(err)
	}
	if err := device.installProbeRoute(); err != ErrIPConflict {
		t.Fatal("accepted occupied probe address", err)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openWindowsPeerDevice(t.Context(), cfg)
	if err != nil {
		t.Fatal("could not reclaim probe addresses after candidate removal", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func testWindowsDriverUDPPolicy(t *testing.T) {
	open := func() *net.UDPConn {
		t.Helper()
		s, err := openWindowsPeerSocket(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	listener, broker, other := open(), open(), open()
	endpoint := func(s *net.UDPConn) netip.AddrPort { return s.LocalAddr().(*net.UDPAddr).AddrPort() }
	ipv6 := func(port uint16) *net.UDPConn {
		s, err := openExclusiveUDP(t.Context(), "udp6", netip.AddrPortFrom(netip.IPv6Loopback(), port).String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	listener6, other6 := ipv6(endpoint(listener).Port()), ipv6(0)
	check := func(from, to *net.UDPConn, allowed bool) {
		t.Helper()
		if err := to.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		_, err := from.WriteToUDPAddrPort([]byte("test"), endpoint(to))
		if err != nil && (allowed || !errors.Is(err, windows.WSAEACCES)) {
			t.Fatal(err)
		}
		var buf [16]byte
		n, source, err := to.ReadFromUDPAddrPort(buf[:])
		if allowed {
			if err != nil || string(buf[:n]) != "test" || source != endpoint(from) {
				t.Fatalf("allowed UDP path failed: %v", err)
			}
		} else if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("blocked UDP path delivered %d bytes: %v", n, err)
		}
	}
	check(other6, listener6, true)
	guard, err := blockWindowsDriverSocket(t.Context(), endpoint(listener).Port(), endpoint(broker))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if guard.handle != 0 {
			if err := guard.close(); err != nil {
				t.Error(err)
			}
		}
	})
	check(broker, listener, true)
	check(listener, broker, true)
	check(other, listener, false)
	check(listener, other, false)
	check(other, broker, true)
	check(other6, listener6, false)
	check(listener6, other6, false)
	if err := guard.close(); err != nil {
		t.Fatal(err)
	}
	check(other, listener, true)
	check(other6, listener6, true)
}

func windowsRoutePrimary(t *testing.T, fixture *candidateFixture) (*WindowsSession, wintun.Session, state.ResourceJournal) {
	t.Helper()
	var relay wintun.Session
	var filtersOwner *WindowsSession
	t.Cleanup(func() {
		if filtersOwner != nil && filtersOwner.filters != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for {
				err := filtersOwner.filters.CloseAfterIPCleanup(ctx, filtersOwner.ip)
				if err == nil {
					break
				}
				if !errors.Is(err, ErrPolicyCleanupRequired) || ctx.Err() != nil {
					t.Errorf("WFP cleanup: %v, remaining direct routes=%d", err, len(filtersOwner.ip.direct))
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	if err := wintundriver.Load(); err != nil {
		t.Fatal(err)
	}
	key, err := canonicalGUID(policySession)
	if err != nil {
		t.Fatal(err)
	}
	record, err := PreparedWindowsAdapter(policySession)
	if err != nil {
		t.Fatal(err)
	}
	tun, err := wintun.CreateAdapter(record.Name, "EndlessNet", &key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tun.Close() })
	relay, err = tun.StartSession(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.End)
	primary := winipcfg.LUID(tun.LUID())
	id, err := (nativeTUNAPI{}).identity(uint64(primary))
	if err != nil {
		t.Fatal(err)
	}
	record.LUID = id.LUID
	record.Phase = state.EffectApplied
	record.Fingerprint = adapterProof(id)
	ip, err := NewWindowsIP(4096)
	if err != nil {
		t.Fatal(err)
	}
	primarySession := &WindowsSession{tunnel: &WindowsTUN{receipt: record}, identity: nativeTUNAPI{}.identity, ip: ip}
	primaryPlan, err := PrepareIPPlan(fixture.verified, policySession, id.LUID, 900, len(fixture.snapshot.Peers))
	if err != nil {
		t.Fatal(err)
	}
	addr, routes := primaryPlan.Prepared()
	journal := state.ResourceJournal{ID: policySession, Generation: fixture.profile.Connection.Generation, Adapter: &record, Addresses: []state.AddressRecord{addr}, Routes: routes}
	effect, err := ip.CreateAddress(t.Context(), primaryPlan, journal)
	if err != nil {
		t.Fatal("primary address", err)
	}
	for until := time.Now().Add(5 * time.Second); !effect.Ready && time.Now().Before(until); {
		time.Sleep(20 * time.Millisecond)
		effect, err = ip.InspectAddress(t.Context(), primaryPlan, effect.Record)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !effect.Ready {
		t.Fatal("primary address failed DAD")
	}
	journal.Addresses[0] = effect.Record
	for i, route := range routes {
		journal.Routes[i], err = ip.CreateRoute(t.Context(), primaryPlan, route, journal)
		if err != nil {
			t.Fatal("primary route", err)
		}
	}
	filtersOwner = primarySession
	return primarySession, relay, journal
}

func verifyWindowsWintunFallback(t *testing.T, app *net.UDPConn, relay wintun.Session, local, peer netip.Addr) {
	t.Helper()
	if _, err := app.Write([]byte("back-on-wintun")); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(time.Second); time.Now().Before(end); {
		pkt, err := relay.ReceivePacket()
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		match := len(pkt) >= 28 && [4]byte(pkt[12:16]) == local.As4() && [4]byte(pkt[16:20]) == peer.As4() && string(pkt[28:]) == "back-on-wintun"
		relay.ReleaseReceivePacket(pkt)
		if match {
			return
		}
	}
	t.Fatal("existing UDP flow did not return to Wintun")
}

func windowsSearchRouteFixture(t *testing.T, f *candidateFixture, config *supervisorConfig) searchRouteFixture {
	t.Helper()
	s, relay, journal := windowsRoutePrimary(t, f)
	prepareWindowsSelectionSession(t, f, s, journal)
	config.selectDirect = s.selectDirect
	refresh := func(m connectivity.Map) {
		s.mu.Lock()
		defer s.mu.Unlock()
		cursor := m.Cursor()
		v, err := s.store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer clearSessionKeys(v)
		if err := s.store.Update(t.Context(), v.Revision, func(v *state.State) error {
			p := v.Profiles[s.profileID]
			p.Connection.MapCursor = &cursor
			v.Profiles[s.profileID] = p
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		s.current = m
	}
	return searchRouteFixture{refresh: refresh, verifyFallback: func(app *net.UDPConn) {
		verifyWindowsWintunFallback(t, app, relay, netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("100.70.0.2"))
	}}
}

// Start from the measured primary adapter, address and route receipts. The
// session uses the protected test store and the guest's real WFP transactions.
func prepareWindowsSelectionSession(t *testing.T, fixture *candidateFixture, s *WindowsSession, journal state.ResourceJournal) {
	t.Helper()
	connection := *fixture.profile.Connection
	cursor := fixture.verified.Cursor()
	connection.MapCursor, connection.Journal = &cursor, &journal
	profile := fixture.profile
	profile.Connection = &connection
	s.store, _ = hintStoreFixture(t, &candidateFixture{profile: profile})
	s.current, s.clock, s.lease = fixture.verified, fixture.clock, fixture.lease
	s.id, s.profileID, s.generation = journal.ID, profile.ID, journal.Generation
	s.lifetime, s.cancel = context.WithCancel(t.Context())
	t.Cleanup(s.cancel)
	s.config = WindowsSessionConfig{ApplyTimeout: 5 * time.Second, RouteMetric: 900, MaxPeers: len(fixture.snapshot.Peers)}
	row, err := s.identity(journal.Adapter.LUID)
	if err != nil {
		t.Fatal(err)
	}
	s.policy, err = PrepareFilterPlan(t.Context(), s.current, s.id, row.LUID, row.Index, 16, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	s.filters, err = NewWindowsFilters(s.id, time.Second, func(ctx context.Context, records []state.FilterRecord) error {
		return s.store.RecordPreparedGuardIDs(ctx, s.profileID, s.id, s.generation, records)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.replaceFilters(t.Context(), s.current, s.policy); err != nil {
		t.Fatal("apply primary WFP", err)
	}
}

func testWindowsPeerExchange(t *testing.T, fixture *candidateFixture, adapter *windowsPeerDevice, config directCipherConfig, external, remoteSocket *net.UDPConn, challenge transport.Control, scenario string) {
	t.Helper()
	a := config.assignment
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan []byte, 4)
	port, err := newPacketPort(t.Context(), 1420, func() connectivity.Permission { return connectivity.Permission{ExistingFlows: true, NewFlows: true} }, func(raw []byte) error {
		select {
		case received <- bytes.Clone(raw):
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	clientAddr := external.LocalAddr().(*net.UDPAddr).AddrPort()
	bind := &windowsTestPeerBind{Bind: conn.NewStdNetBind(), link: windowsPeerLink{socket: remoteSocket, driver: clientAddr}}
	remote := device.NewDevice(wireGuardPort{port}, bind, device.NewLogger(device.LogLevelSilent, ""))
	defer remote.Close()
	var measure atomic.Bool
	var probeWire, appWire atomic.Int64
	if scenario == "maintenance" || scenario == "source" || scenario == "routed" {
		probeSource, _, err := a.controlAddresses()
		if err != nil {
			t.Fatal(err)
		}
		remote.SetPacketHooks(&device.PacketHooks{Inbound: func(ip []byte, cipherBytes int) error {
			if measure.Load() && len(ip) >= 28 {
				if [4]byte(ip[12:16]) == probeSource.As4() {
					probeWire.Add(int64(cipherBytes + 28))
				} else {
					appWire.Add(int64(cipherBytes + 28))
				}
			}
			return nil
		}})
	}
	cfg, err := cipherConfiguration(coordinator.Node{PublicKey: a.peer.PublicKey, AssignedIP: peer.String()}, coordinator.Peer{PublicKey: a.local.PublicKey, ACLRestricted: true, AllowedIPs: []string{local.String() + "/32"}, Transport: &coordinator.PeerTransportPlan{LocalProbeIPv4: a.plan.PeerProbeIPv4, PeerProbeIPv4: a.plan.LocalProbeIPv4}}, bytes.Repeat([]byte{84}, 32), a.offer.Profile)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(cfg)
	if err := remote.IpcSetOperation(bytes.NewReader(cfg)); err != nil {
		t.Fatal(err)
	}
	if err := remote.IpcSetOperation(bytes.NewBufferString(fmt.Sprintf("listen_port=%d\n", remoteSocket.LocalAddr().(*net.UDPAddr).Port))); err != nil {
		t.Fatal(err)
	}
	luid := adapter.LUID()
	var relay wintun.Session
	var primarySession *WindowsSession
	var journal state.ResourceJournal
	var directRow *windowsDirectRoute
	if scenario == "routed" {
		primarySession, relay, journal = windowsRoutePrimary(t, fixture)
	} else {
		if err := luid.AddIPAddress(netip.PrefixFrom(local, 32)); err != nil {
			t.Fatal(err)
		}
		if err := luid.AddRoute(netip.PrefixFrom(peer, 32), netip.IPv4Unspecified(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.activate(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := adapter.SetAdapterState(driver.AdapterStateDown); err != nil {
			t.Error(err)
		}
	}()
	installed, err := adapter.Configuration()
	if err != nil {
		t.Fatal(err)
	}
	clear(installed.PrivateKey[:])
	if installed.ListenPort == 0 {
		t.Fatal("driver did not report active listener")
	}
	link, err := adapter.activeLink()
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Up(); err != nil {
		t.Fatal(err)
	}
	pub, err := base64.StdEncoding.DecodeString(a.local.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.IpcSetOperation(bytes.NewBufferString(fmt.Sprintf("public_key=%x\nendpoint=%s\n\n", pub, clientAddr))); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(15 * time.Second)
	var app *net.UDPConn
	for time.Now().Before(until) {
		app, err = net.DialUDP("udp4", &net.UDPAddr{IP: local.AsSlice()}, &net.UDPAddr{IP: peer.AsSlice(), Port: 443})
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	candidate, err := newCandidateBind(t.Context(), a, config.clock)
	if err != nil {
		t.Fatal(err)
	}
	observed := &windowsObservedSocket{UDPConn: external, observed: make(chan netip.AddrPort, 1)}
	candidate.listen = func(uint16) (candidateSocket, error) { return observed, nil }
	_, _, err = candidate.Open(clientAddr.Port())
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.shutdown()
	cycle, err := newProbeCycle(t.Context(), config.clock, "windows-test", a.plan.LocalIntentGeneration, "windows-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cycle.cancel()
	attempt, err := cycle.attempt("windows-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.beginProbe(attempt); err != nil {
		t.Fatal(err)
	}
	broker, err := adapter.attachCandidate(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	deniedSend := make(chan error, 1)
	workers.Go(func() { _ = broker.receive() })
	workers.Go(func() {
		_ = broker.send(func(err error) {
			if observed.watch.Load() {
				select {
				case deniedSend <- err:
				default:
				}
			}
		})
	})
	defer func() { candidate.shutdown(); link.socket.SetReadDeadline(time.Now()); workers.Wait() }()
	payload := []byte("native windows")
	if _, err := app.Write([]byte("blocked by candidate WFP")); err != nil && !errors.Is(err, windows.WSAEACCES) {
		t.Fatal(err)
	}
	select {
	case <-received:
		t.Fatal("application packet escaped the prepared candidate")
	case <-time.After(300 * time.Millisecond):
	}
	// Establish a real incoming encrypted flow while the preparation rules
	// are still active. Verify driver receipt and absence at the app socket.
	incoming := cipherIPv4(peer.String(), local.String(), 77)
	binary.BigEndian.PutUint16(incoming[20:22], 443)
	binary.BigEndian.PutUint16(incoming[22:24], uint16(app.LocalAddr().(*net.UDPAddr).Port))
	var receivedByDriver bool
	var baseline uint64
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		if err := port.enqueue(incoming, false); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		c, err := adapter.Configuration()
		if err != nil {
			t.Fatal(err)
		}
		clear(c.PrivateKey[:])
		p := c.FirstPeer()
		if p.LastHandshake == 0 {
			continue
		}
		if baseline != 0 && p.RxBytes > baseline {
			receivedByDriver = true
			break
		}
		baseline = p.RxBytes
	}
	if !receivedByDriver {
		t.Fatal("driver did not receive encrypted data under WFP blocks")
	}
	if err := app.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var blocked [64]byte
	if n, err := app.Read(blocked[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("prepared candidate delivered inbound bytes=%d, err=%v", n, err)
	}
	if err := adapter.beginRoutedProbe(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	probeLocal, probePeer, err := a.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	testWindowsRoutedControl(t, adapter.probeSocket, challenge, received, port)
	if scenario == "maintenance" || scenario == "source" || scenario == "routed" {
		snapshot, err := a.mapValue.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		row, err := adapter.LUID().Interface()
		if err != nil {
			t.Fatal(err)
		}
		identity := adapterInfo(*row)
		if scenario == "routed" {
			if err := adapter.prepareApplicationIP(identity); err != nil {
				t.Fatal(err)
			}
			prepareWindowsSelectionSession(t, fixture, primarySession, journal)
			directRow, err = primarySession.stageDirectIP(t.Context(), a, identity, adapter.probeGuard.close)
			if err != nil || !primarySession.weakSendOwned {
				t.Fatal("session direct selection", err)
			}
		} else {
			// Exercise the real candidate rules in a disposable sublayer. The
			// primary interface only supplies the signed grants used to build them.
			primary, err := filterPlan(snapshot, policySession, 12345, 12345, 16, 16, false)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := primary.withConfirmedPeer(a, identity, false)
			if err != nil {
				t.Fatal(err)
			}
			var rules []*wf.Rule
			for _, r := range selected.rules {
				if selected.direct[identity.LUID].rules[r.ID] {
					rules = append(rules, r)
				}
			}
			active, err := installCandidateBlocks(t.Context(), rules)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := active.close(); err != nil {
					t.Error(err)
				}
			}()
			if err := adapter.probeGuard.close(); err != nil {
				t.Fatal(err)
			}
		}
		adapter.probeGuard = nil
		challenge.Nonce = bytes.Clone(challenge.Nonce)
		challenge.Nonce[0]++
		meter, err := newProbeCycle(t.Context(), config.clock, "windows-test", a.plan.LocalIntentGeneration, "maintenance")
		if err != nil {
			t.Fatal(err)
		}
		defer meter.cancel()
		// Both directions now use confirmed admission in the real bind. The
		// fixture supplies this state independently of supervisor negotiation.
		candidate.mu.Lock()
		candidate.receiveConfirmed, candidate.transmitConfirmed = true, true
		candidate.adapterProbeOnly = false
		candidate.confirmationEpoch++
		candidate.mu.Unlock()
		measure.Store(true)
		testWindowsMaintenanceWithApplication(t, adapter.probeSocket, challenge, app, received, port, meter)
		if scenario == "routed" {
			fresh, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: peer.AsSlice(), Port: 443})
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if addr := fresh.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(); addr != local {
				t.Fatalf("automatic source %v, want %v", addr, local)
			}
			if _, err := fresh.Write([]byte("fresh-direct")); err != nil {
				t.Fatal(err)
			}
			select {
			case pkt := <-received:
				if len(pkt) < 28 || [4]byte(pkt[12:16]) != local.As4() || string(pkt[28:]) != "fresh-direct" {
					t.Fatal("incorrect application source at decrypted peer")
				}
			case <-time.After(time.Second):
				t.Fatal("fresh UDP failed to reach the encrypted peer")
			}
			if err := primarySession.retireDirectIP(t.Context(), peer, directRow); err != nil {
				t.Fatal(err)
			}
			if primarySession.weakSendOwned {
				t.Fatal("Wintun source selection was not restored")
			}
			verifyWindowsWintunFallback(t, fresh, relay, local, peer)
			return
		}
		if used := int64(meter.wire.consumed()); used == 0 || used != probeWire.Load() || appWire.Load() != used {
			t.Fatalf("reserved=%d, independently observed probe=%d application=%d", used, probeWire.Load(), appWire.Load())
		}
		t.Logf("outgoing reservation=%d, authenticated probe=%d, application=%d", meter.wire.consumed(), probeWire.Load(), appWire.Load())
		testWindowsInboundPadding(t, adapter.probeSocket, port, observed, broker, challenge)
		challenge.Nonce[0]++
		pendingProbe, err := controlPacket(probeLocal, probePeer, challenge)
		if err != nil {
			t.Fatal(err)
		}
		observed.watch.Store(true)
		if scenario == "source" {
			testWindowsQueuedCiphertext(t, observed, deniedSend, func() error {
				return adapter.probeSocket.send(pendingProbe, func(n int) error { return meter.charge(n + 28) })
			}, func() {
				a.lease.PausePackets(true)
				a.lease.PausePackets(false)
			}, connectivity.ErrSourceChanged)
			// Even ciphertext first read from the adapter after source recovery
			// belongs to the retired preparation.
			writes := observed.writes.Load()
			if _, err := app.Write([]byte("old-adapter-after-resume")); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-deniedSend:
				if !errors.Is(err, connectivity.ErrSourceChanged) || writes != observed.writes.Load() {
					t.Fatal("retired adapter emitted ciphertext", err)
				}
			case <-time.After(time.Second):
				t.Fatal("no ciphertext from retired adapter")
			}
			return
		}
		before := probeWire.Load()
		if err := meter.charge(transport.MaxProbeBytes - meter.wire.consumed() - 1); err != nil {
			t.Fatal(err)
		}
		challenge.Nonce[0]++
		probe, err := controlPacket(probeLocal, probePeer, challenge)
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.probeSocket.send(probe, func(n int) error { return meter.charge(n + 28) }); !errors.Is(err, ErrProbeBudget) {
			t.Fatal("did not refuse the over-budget maintenance probe", err)
		}
		if _, err := app.Write([]byte("after-quota")); err != nil {
			t.Fatal(err)
		}
		select {
		case pkt := <-received:
			if len(pkt) < 28 || string(pkt[28:]) != "after-quota" {
				t.Fatal("unexpected datagram after exhausting the allowance")
			}
		case <-time.After(time.Second):
			t.Fatal("quota exhaustion interrupted application traffic")
		}
		if meter.wire.consumed() != transport.MaxProbeBytes-1 || probeWire.Load() != before {
			t.Fatal("rejected maintenance packet consumed or transmitted bytes")
		}
		testWindowsQueuedCiphertext(t, observed, deniedSend, func() error {
			_, err := app.Write([]byte("revoked-before-wire"))
			return err
		}, a.lease.Revoke, ErrNoAuthority)
		return
	}
	if scenario != "" {
		used := cycle.wire.consumed()
		if used == 0 || uint64(used) != observed.inBytes.Load()+observed.outBytes.Load() {
			t.Fatalf("probe ledger=%d, incoming=%d, outgoing=%d", used, observed.inBytes.Load(), observed.outBytes.Load())
		}
		// A prior series of probes can leave one byte in this same profile's ledger.
		if err := cycle.charge(transport.MaxProbeBytes - used - 1); err != nil {
			t.Fatal(err)
		}
		if scenario == "incoming" {
			before, err := adapter.Configuration()
			if err != nil {
				t.Fatal(err)
			}
			clear(before.PrivateKey[:])
			reply, err := controlPacket(probePeer, probeLocal, replyControl(challenge, transport.Echo))
			if err != nil {
				t.Fatal(err)
			}
			if err := port.enqueue(reply, true); err != nil {
				t.Fatal(err)
			}
			select {
			case <-attempt.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("encrypted input did not exhaust probe allowance")
			}
			after, err := adapter.Configuration()
			if err != nil {
				t.Fatal(err)
			}
			clear(after.PrivateKey[:])
			if !errors.Is(attempt.check(), ErrProbeBudget) || after.FirstPeer().RxBytes != before.FirstPeer().RxBytes || cycle.wire.consumed() != transport.MaxProbeBytes-1 {
				t.Fatal("over-budget input reached the driver or lost its failure reason")
			}
			t.Logf("incoming budget gate checked against %d observed wire bytes", used)
			return
		}
		before := observed.outBytes.Load()
		observed.watch.Store(true)
		packet, err := controlPacket(probeLocal, probePeer, challenge)
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.probeSocket.send(packet, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deniedSend:
		case <-time.After(time.Second):
			t.Fatal("no denial at encrypted send gate")
		}
		if !errors.Is(attempt.check(), ErrProbeBudget) || observed.outBytes.Load() != before ||
			cycle.wire.consumed() != transport.MaxProbeBytes-1 {
			t.Fatal("over-budget datagram escaped or lost its failure reason")
		}
		t.Logf("pre-confirmation ledger matches both UDP directions with headers: %d bytes", used)
		return
	}
	bad, err := net.DialUDP("udp4", &net.UDPAddr{IP: probeLocal.AsSlice()}, &net.UDPAddr{IP: probePeer.AsSlice(), Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()
	for _, socket := range []*net.UDPConn{app, bad} {
		if _, err := socket.Write(payload); err != nil && !errors.Is(err, windows.WSAEACCES) {
			t.Fatal(err)
		}
		select {
		case <-received:
			t.Fatal("application UDP escaped during the probe")
		case <-time.After(200 * time.Millisecond):
		}
	}
	// Send the identical valid UDP packet before and after removing the probe
	// guard. The positive control distinguishes a filter drop from a broken route.
	invalidPortPacket := cipherIPv4(probePeer.String(), probeLocal.String(), 77)
	binary.BigEndian.PutUint16(invalidPortPacket[20:22], 443)
	binary.BigEndian.PutUint16(invalidPortPacket[22:24], uint16(bad.LocalAddr().(*net.UDPAddr).Port))
	clear(invalidPortPacket[26:28])
	checkServiceUDP := func(blocked bool) {
		t.Helper()
		if err := port.enqueue(invalidPortPacket, false); err != nil {
			t.Fatal(err)
		}
		if err := bad.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		var raw [64]byte
		n, err := bad.Read(raw[:])
		if blocked {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("inbound UDP during probe: bytes=%d err=%v", n, err)
			}
		} else if err != nil || !bytes.Equal(raw[:n], invalidPortPacket[28:]) {
			t.Fatal("positive control for inbound service UDP failed", err)
		}
	}
	checkServiceUDP(true)
	// This isolated exchange exercises the driver directly. The test owns the
	// sole remote /32 and explicitly removes the preparation blocks here.
	if err := adapter.probeGuard.close(); err != nil {
		t.Fatal(err)
	}
	adapter.probeGuard = nil
	checkServiceUDP(false)
	var reply []byte
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end) && reply == nil; {
		if _, err := app.Write(payload); err != nil {
			t.Fatal(err)
		}
		select {
		case reply = <-received:
		case <-time.After(500 * time.Millisecond):
		}
	}
	if reply == nil {
		c, err := adapter.Configuration()
		if err != nil {
			t.Fatal(err)
		}
		clear(c.PrivateKey[:])
		p := c.FirstPeer()
		t.Fatalf("Go peer did not receive traffic: Go socket received=%d sent=%d; native rx=%d tx=%d handshake=%t", bind.received.Load(), bind.sent.Load(), p.RxBytes, p.TxBytes, p.LastHandshake != 0)
	}
	if len(reply) != 28+len(payload) || !bytes.Equal(reply[28:], payload) {
		t.Fatal("incorrect plaintext from Windows")
	}
	copy(reply[12:16], peer.AsSlice())
	copy(reply[16:20], local.AsSlice())
	source := binary.BigEndian.Uint16(reply[20:22])
	binary.BigEndian.PutUint16(reply[20:22], 443)
	binary.BigEndian.PutUint16(reply[22:24], source)
	clear(reply[26:28])
	copy(reply[28:], "reply from Go!")
	if err := port.enqueue(reply, false); err != nil {
		t.Fatal(err)
	}
	if err := app.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var raw [64]byte
	n, err := app.Read(raw[:])
	if err != nil || string(raw[:n]) != "reply from Go!" {
		t.Fatalf("native return path length=%d, err=%v", n, err)
	}
	// Start a distinct attempt in the same byte budget so time spent proving
	// bidirectional delivery cannot make a timeout look like revocation.
	attempt, err = cycle.attempt("revocation")
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.beginProbe(attempt); err != nil {
		t.Fatal(err)
	}
	a.lease.Revoke()
	before, err := adapter.Configuration()
	if err != nil {
		t.Fatal(err)
	}
	clear(before.PrivateKey[:])
	observed.watch.Store(true)
	if err := port.enqueue(reply, false); err != nil {
		t.Fatal(err)
	}
	select {
	case addr := <-observed.observed:
		if addr.String() != a.peerEndpoint {
			t.Fatal("unexpected sender after revocation")
		}
	case <-time.After(time.Second):
		t.Fatal("no datagram reached the external socket after revocation")
	}
	if err := app.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if n, e := app.Read(raw[:]); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatalf("received %d bytes after revocation: %v", n, e)
	}
	// The test has lifted the inner interface block. A local packet can
	// still reach the driver, but the public socket must reject its output.
	writes := observed.writes.Load()
	if _, err := app.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-deniedSend:
		if !errors.Is(err, ErrNoAuthority) && !errors.Is(err, connectivity.ErrSourceChanged) {
			t.Fatal("unexpected rejection after revocation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("driver output did not reach the revoked send gate")
	}
	after, err := adapter.Configuration()
	if err != nil {
		t.Fatal(err)
	}
	clear(after.PrivateKey[:])
	if after.FirstPeer().RxBytes != before.FirstPeer().RxBytes {
		t.Fatal("driver received encrypted bytes after revocation")
	}
	if after.FirstPeer().TxBytes <= before.FirstPeer().TxBytes || observed.writes.Load() != writes || attempt.check() != nil {
		t.Fatalf("revocation fence: driver tx %d -> %d, public writes %d -> %d, attempt error %v", before.FirstPeer().TxBytes, after.FirstPeer().TxBytes, writes, observed.writes.Load(), attempt.check())
	}
}

func testWindowsInboundPadding(t *testing.T, socket *windowsProbeSocket, port *packetPort, observed *windowsObservedSocket, broker *windowsCandidateBroker, challenge transport.Control) {
	t.Helper()
	echo, err := controlPacket(socket.peer, socket.local, replyControl(challenge, transport.Echo))
	if err != nil {
		t.Fatal(err)
	}
	// Both messages have the same IPv4 total length and UDP checksum.
	// The peer encrypts 32 additional zero bytes after the second IP packet.
	var wire [2]uint64
	for i, packet := range [][]byte{echo, append(bytes.Clone(echo), make([]byte, 32)...)} {
		before := observed.inBytes.Load()
		if err := port.enqueue(packet, true); err != nil {
			t.Fatal(err)
		}
		var raw [28 + transport.MaxDatagramBytes]byte
		n, err := socket.receive(raw[:])
		if err != nil || !bytes.Equal(raw[:n], echo) {
			t.Fatalf("inbound echo %d: length=%d err=%v", i, n, err)
		}
		wire[i] = observed.inBytes.Load() - before
		if upper := int64(broker.largestIngress.Load()) + 28; upper < int64(wire[i]) {
			t.Fatalf("ingress bound %d below observed %d", upper, wire[i])
		}
	}
	if wire[1] != wire[0]+32 {
		t.Fatalf("same authenticated IP packet, unexpected wire sizes %v", wire)
	}
	t.Logf("identical %d-byte authenticated echoes, external IPv4/UDP bytes %v", len(echo), wire)
}

func testWindowsMaintenanceWithApplication(t *testing.T, socket *windowsProbeSocket, challenge transport.Control, app *net.UDPConn, received <-chan []byte, port *packetPort, meter *probeCycle) {
	t.Helper()
	request, err := controlPacket(socket.local, socket.peer, challenge)
	if err != nil {
		t.Fatal(err)
	}
	// Equal inner lengths prevent a ciphertext-size heuristic from telling
	// the application and maintenance datagrams apart.
	body := bytes.Repeat([]byte{0x6b}, len(request)-28)
	sent := make(chan error, 1)
	go func() {
		_, err := app.Write(body)
		sent <- err
	}()
	if err := socket.send(request, func(n int) error { return meter.charge(n + 28) }); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	var sawData, sawProbe bool
	deadline := time.After(3 * time.Second)
	for !sawData || !sawProbe {
		select {
		case pkt := <-received:
			if len(pkt) != len(request) {
				continue
			}
			if bytes.Equal(pkt[28:], request[28:]) {
				value, claimed, err := decodeControlPacket(pkt, socket.local, socket.peer)
				if err != nil || !claimed || !sameControl(challenge, value) {
					t.Fatal("invalid maintenance challenge", err)
				}
				echo, err := controlPacket(socket.peer, socket.local, replyControl(value, transport.Echo))
				if err != nil {
					t.Fatal(err)
				}
				if err := port.enqueue(echo, true); err != nil {
					t.Fatal(err)
				}
				sawProbe = true
			} else if bytes.Equal(pkt[28:], body) {
				reply := bytes.Clone(pkt)
				copy(reply[12:16], pkt[16:20])
				copy(reply[16:20], pkt[12:16])
				copy(reply[20:22], pkt[22:24])
				copy(reply[22:24], pkt[20:22])
				clear(reply[26:28])
				if err := port.enqueue(reply, false); err != nil {
					t.Fatal(err)
				}
				sawData = true
			}
		case <-deadline:
			t.Fatalf("concurrent exchange: application=%t probe=%t", sawData, sawProbe)
		}
	}
	var raw [28 + transport.MaxDatagramBytes]byte
	n, err := socket.receive(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	value, claimed, err := decodeControlPacket(raw[:n], socket.peer, socket.local)
	if err != nil || !claimed || !transport.MatchesReply(challenge, value) {
		t.Fatal("missing maintenance echo", err)
	}
	if err := app.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err = app.Read(raw[:])
	if err != nil || !bytes.Equal(raw[:n], body) {
		t.Fatal("missing application echo", err)
	}
}

// This exchange runs with the peer route already on WireGuardNT. It exercises
// the wire codec and checks which interface delivered the reply to Windows.
func testWindowsRoutedControl(t *testing.T, socket *windowsProbeSocket, challenge transport.Control, atPeer <-chan []byte, port *packetPort) {
	t.Helper()
	local, peer := socket.local, socket.peer
	request, err := controlPacket(local, peer, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if err := socket.send(request, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-atPeer:
		value, claimed, err := decodeControlPacket(raw, local, peer)
		if !claimed || err != nil || value.Kind != transport.Challenge {
			t.Fatalf("invalid decrypted challenge: %v", err)
		}
		echo, err := controlPacket(peer, local, replyControl(value, transport.Echo))
		if err != nil {
			t.Fatal(err)
		}
		if err := port.enqueue(echo, true); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Go peer did not receive the routed challenge")
	}
	var raw [28 + transport.MaxDatagramBytes]byte
	n, err := socket.receive(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	value, claimed, err := decodeControlPacket(raw[:n], peer, local)
	if !claimed || err != nil || !transport.MatchesReply(challenge, value) {
		t.Fatalf("invalid routed echo: %v", err)
	}
}

// Hold the encrypted packet at the external socket, after the bind has
// admitted it and before the final lease check preceding the UDP write.
func testWindowsQueuedCiphertext(t *testing.T, socket *windowsObservedSocket, denied <-chan error, send func() error, transition func(), want error) {
	t.Helper()
	wait := &windowsWritePause{entered: make(chan struct{}), resume: make(chan struct{})}
	socket.pause.Store(wait)
	var release sync.Once
	defer release.Do(func() { close(wait.resume) })
	before := socket.writes.Load()
	if err := send(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wait.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("driver ciphertext did not reach the external write gate")
	}
	transition()
	release.Do(func() { close(wait.resume) })
	select {
	case err := <-denied:
		if !errors.Is(err, want) || socket.writes.Load() != before {
			t.Fatalf("queued ciphertext: err=%v writes=%d before=%d", err, socket.writes.Load(), before)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued write was not rejected")
	}
}

type windowsWritePause struct {
	entered chan struct{}
	resume  chan struct{}
}

type windowsObservedSocket struct {
	pause atomic.Pointer[windowsWritePause]
	*net.UDPConn
	watch             atomic.Bool
	writes            atomic.Uint64
	inBytes, outBytes atomic.Uint64
	observed          chan netip.AddrPort
}

func (s *windowsObservedSocket) SetWriteDeadline(deadline time.Time) error {
	if pause := s.pause.Swap(nil); pause != nil {
		close(pause.entered)
		<-pause.resume
	}
	return s.UDPConn.SetWriteDeadline(deadline)
}

func (s *windowsObservedSocket) WriteToUDPAddrPort(raw []byte, addr netip.AddrPort) (int, error) {
	n, err := s.UDPConn.WriteToUDPAddrPort(raw, addr)
	if n > 0 {
		s.outBytes.Add(uint64(n + 28))
		s.writes.Add(1)
	}
	return n, err
}

func (s *windowsObservedSocket) ReadFromUDPAddrPort(buf []byte) (int, netip.AddrPort, error) {
	n, addr, err := s.UDPConn.ReadFromUDPAddrPort(buf)
	if err == nil && n > 0 {
		s.inBytes.Add(uint64(n + 28))
	}
	if err == nil && s.watch.Load() {
		select {
		case s.observed <- addr:
		default:
		}
	}
	return n, addr, err
}

// Use the signed guest underlay endpoints for the Go peer. Counters distinguish
// failure to reach the socket from a rejected handshake in either engine.
type windowsTestPeerBind struct {
	conn.Bind
	link           windowsPeerLink
	opened         atomic.Bool
	received, sent atomic.Uint32
}

func (b *windowsTestPeerBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	if b.link.socket == nil || b.link.socket.LocalAddr().(*net.UDPAddr).Port != int(port) || !b.opened.CompareAndSwap(false, true) {
		return nil, 0, ErrEngine
	}
	if err := b.link.socket.SetReadDeadline(time.Time{}); err != nil {
		return nil, 0, err
	}
	return []conn.ReceiveFunc{func(raw []byte, packets []conn.ReceivedPacket) (int, error) {
		n, err := b.link.read(raw)
		if err != nil {
			if !b.opened.Load() {
				return 0, net.ErrClosed
			}
			return 0, err
		}
		b.received.Add(1)
		ep, err := b.ParseEndpoint(b.link.driver.String())
		if err != nil {
			return 0, err
		}
		packets[0] = conn.ReceivedPacket{Size: n, Endpoint: ep}
		return 1, nil
	}}, port, nil
}
func (*windowsTestPeerBind) BatchSize() int { return 1 }
func (b *windowsTestPeerBind) Close() error {
	if b.opened.Swap(false) {
		return b.link.socket.SetReadDeadline(time.Now())
	}
	return nil
}
func (b *windowsTestPeerBind) Send(bufs [][]byte, ep conn.Endpoint, offset int) error {
	if !b.opened.Load() {
		return net.ErrClosed
	}
	addr, err := netip.ParseAddrPort(ep.DstToString())
	if err != nil {
		return err
	}
	if addr != b.link.driver {
		return ErrNoAuthority
	}
	for _, raw := range bufs {
		if err := b.link.write(raw[offset:]); err != nil {
			return err
		}
		b.sent.Add(1)
	}
	return nil
}
