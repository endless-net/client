package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relaywire "github.com/endless-net/relay/api/wire"
)

func threeSearchFixtures(t *testing.T, routed bool) []*candidateFixture {
	t.Helper()
	a := newCandidateEngineFixture(t, transport.WireGuard)
	first := &a.snapshot.Peers[0]
	second := *first
	plan := *first.Transport
	second.Transport = &plan
	key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{35}, 32))
	if err != nil {
		t.Fatal(err)
	}
	second.ID, second.PublicKey = "third", base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	second.Endpoint = "192.0.2.30:51820"
	second.AllowedIPs = []string{"100.70.0.3/32"}
	second.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: second.AllowedIPs, AllowedPorts: first.ACLGrants[0].AllowedPorts}}
	if routed {
		first.Transport.LocalProbeIPv4, first.Transport.PeerProbeIPv4 = "172.20.4.10", "172.20.4.11"
		plan.LocalProbeIPv4, plan.PeerProbeIPv4 = "172.20.4.12", "172.20.4.13"
		first.ACLGrants[0].AllowedPorts = append(first.ACLGrants[0].AllowedPorts, coordinator.ACLPort{Protocol: "udp", Port: 443})
	}
	plan.PeerNodeID, plan.PeerPublicKey = second.ID, second.PublicKey
	awg := cipherProfile(transport.AmneziaWG)
	awg.ID = "obfuscated"
	digest, err := transport.ProfileDigest(awg)
	if err != nil {
		t.Fatal(err)
	}
	plan.Profiles = []coordinator.TransportProfileOffer{{Profile: awg, Digest: digest, LocalEndpoints: []string{"192.0.2.10:51822"}, PeerEndpoints: []string{second.Endpoint}}}
	plan.PeerAdvertisement.Capabilities = TransportCapabilities()
	plan.PeerAdvertisement.Endpoints = []coordinator.TransportEndpoint{{ProfileID: awg.ID, ProfileDigest: digest, Endpoint: second.Endpoint}}
	ad := a.snapshot.Node.Transport
	ad.Capabilities = TransportCapabilities()
	ad.Endpoints = append(ad.Endpoints, coordinator.TransportEndpoint{ProfileID: awg.ID, ProfileDigest: digest, Endpoint: plan.Profiles[0].LocalEndpoints[0]})
	a.snapshot.Peers = append(a.snapshot.Peers, second)
	previous := a.verified.Cursor()
	a.clock.advance(time.Millisecond, time.Millisecond)
	a.snapshot.Revision.Network++
	a.snapshot.Network.Revision++
	a.refresh(t, &previous)
	return []*candidateFixture{a, reverseSearchFixture(t, a), reverseSearchPeer(t, a, second, key.Bytes())}
}

// The hub is one Client with one packet switch, lease and transport budget.
// Each remote has its own supervisor, signed map, keys and Relay session.
func TestThreeSupervisorsIsolateDirectLossAndRevocation(t *testing.T) {
	goBackend := searchTestBackend{newTransportBudget, (*peerCipher)(nil)}
	testThreeSearchPeers(t, goBackend, (*peerCipher)(nil), goBackend, memoryThreePeerLinks())
}

func testThreeSearchPeers(t *testing.T, hubWG searchTestBackend, hubAWG directPeerEngine, thirdBackend searchTestBackend, links threePeerLinks) {
	t.Helper()
	fixtures := threeSearchFixtures(t, links.setupHub != nil)
	// The signed clock must keep pace with socket deadlines during native DAD,
	// filter transactions and route retirement, including between actor ticks.
	if links.setupHub != nil {
		fixtures[0].clock.startRealTime()
	}
	var hostRoute searchRouteFixture
	type node struct {
		supervisor *transportSupervisor
		packets    *packetSwitch
		tun        *switchTUN
		frames     <-chan *relaywire.ClientFrame
		inject     func(string, []byte)
	}
	nodes := make(map[string]*node)
	for i, f := range fixtures {
		id := f.snapshot.Node.ID
		if links.setupHub != nil {
			f.mapTTL = 2 * time.Minute
			now, _ := f.clock.Sample()
			expires := now.Wall.Add(3 * time.Minute)
			f.snapshot.Node.EndpointExpiresAt = &expires
			for j := range f.snapshot.Peers {
				f.snapshot.Peers[j].EndpointExpiresAt = &expires
			}
			f.withRelayLifetime(t, "relay.example:443", f.mapTTL)
		} else {
			f.withRelay(t)
		}
		config := supervisorConfig{probeInterval: 500 * time.Millisecond, retry: time.Second, mtu: 576, maxPeers: 3}
		if i == 0 && links.setupHub != nil {
			hostRoute = links.setupHub(t, f, &config)
		}
		relay, _, frames, inject := relayPipeFixtureFor(t, f)
		config.relay = relay.config
		packets, native := switchFixture(t, f)
		backend := searchTestBackend{newTransportBudget, (*peerCipher)(nil)}
		switch i {
		case 0:
			backend = hubWG
		case 2:
			backend = thirdBackend
		}
		budget, err := backend.makeBudget(3)
		if err != nil {
			t.Fatal(err)
		}
		slot, err := budget.reserve(f.lease, "", enginePrimary)
		if err != nil {
			t.Fatal(err)
		}
		primary, err := NewEngine(wireGuardPort{packets.primary}, &inertBind{}, 3, 51820, f.lease)
		if err != nil {
			t.Fatal(err)
		}
		primary.reservation = slot
		t.Cleanup(primary.Close)
		if err := primary.Apply(t.Context(), f.verified, f.profile.Keys.WireGuardPrivate); err != nil {
			t.Fatal(err)
		}
		listen := func(_ string, profile string, _ uint16) (candidateSocket, error) {
			for _, endpoint := range f.snapshot.Node.Transport.Endpoints {
				if endpoint.ProfileID != profile {
					continue
				}
				addr := netip.MustParseAddrPort(endpoint.Endpoint)
				return links.listen(addr, profile)
			}
			return nil, ErrConfiguration
		}
		config.listenCandidate = listen
		g, err := newTransportSupervisor(t.Context(), f.verified, f.lease, f.clock, f.profile.ID, f.profile.Connection.Generation, f.profile.Keys.WireGuardPrivate, budget, packets, primary, config, func(context.Context, connectivity.Map, string) (*RelayTransport, error) { return relay, nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(g.Close)
		nodes[id] = &node{g, packets, native, frames, inject}
	}
	var relayHandshakes atomic.Uint64
	for id, n := range nodes {
		go func() {
			for {
				select {
				case <-t.Context().Done():
					return
				case frame := <-n.frames:
					if ((id == "node" && frame.PeerID == "peer") || (id == "peer" && frame.PeerID == "node")) && wireGuardHandshakePacket(frame.Payload) {
						relayHandshakes.Add(1)
					}
					if peer := nodes[frame.PeerID]; peer != nil {
						peer.inject(id, frame.Payload)
					}
				}
			}
		}()
	}
	a, b, c := nodes["node"], nodes["peer"], nodes["third"]
	last := time.Now()
	wait := func(ready func() bool) {
		t.Helper()
		for end := time.Now().Add(25 * time.Second); !ready(); {
			now := time.Now()
			if now.After(end) {
				for _, f := range fixtures {
					t.Logf("%s transport %v", f.snapshot.Node.ID, nodes[f.snapshot.Node.ID].supervisor.transportStatuses(f.verified))
				}
				t.Fatalf("routes: A=%v B=%v C=%v", a.supervisor.outputChoices(), b.supervisor.outputChoices(), c.supervisor.outputChoices())
			}
			elapsed := now.Sub(last)
			last = now
			if links.setupHub == nil {
				fixtures[0].clock.advance(elapsed, elapsed)
			}
			for _, n := range nodes {
				n.supervisor.notify()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait(func() bool {
		return a.supervisor.outputChoices()["peer"] == peerRouteDirect && a.supervisor.outputChoices()["third"] == peerRouteDirect && b.supervisor.outputChoices()["node"] == peerRouteDirect && c.supervisor.outputChoices()["node"] == peerRouteDirect
	})
	offerB, offerC := fixtures[0].snapshot.Peers[0].Transport.Profiles[0], fixtures[0].snapshot.Peers[1].Transport.Profiles[0]
	ab := checkSelectedPeerEngine(t, a.packets, "node", "peer", offerB, hubWG.expected)
	checkSelectedPeerEngine(t, b.packets, "peer", "node", offerB, (*peerCipher)(nil))
	ac := checkSelectedPeerEngine(t, a.packets, "node", "third", offerC, hubAWG)
	ca := checkSelectedPeerEngine(t, c.packets, "third", "node", offerC, thirdBackend.expected)
	exchange := func(remote *node, ip string, marker byte) {
		t.Helper()
		forward, reverse := cipherIPv4("100.70.0.1", ip, marker), cipherIPv4(ip, "100.70.0.1", marker+1)
		a.tun.input <- forward
		remote.tun.input <- reverse
		for _, dir := range []struct {
			n   *node
			raw []byte
		}{{remote, forward}, {a, reverse}} {
			select {
			case got := <-dir.n.tun.output:
				if !bytes.Equal(got, dir.raw) {
					t.Fatal("crossed peer payload")
				}
			case <-time.After(time.Second):
				t.Fatalf("lost payload for %s", ip)
			}
		}
	}
	// Exercise each observable phase of native apply. Leave the prior signed
	// map installed while transferring packets, then let each owner adopt the
	// new map independently, as the production worker and supervisor do.
	checkChurn := func(path peerRouteKind) {
		t.Helper()
		f := fixtures[0]
		key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{59}, 32))
		if err != nil {
			t.Fatal(err)
		}
		observer := f.snapshot.Peers[0]
		observer.ID, observer.PublicKey, observer.Endpoint, observer.Transport = "observer", base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), "192.0.2.77:51820", nil
		observer.AllowedIPs = []string{"100.70.0.4/32"}
		observer.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: observer.AllowedIPs, AllowedPorts: observer.ACLGrants[0].AllowedPorts}}
		handshakes := relayHandshakes.Load
		if path == peerRouteDirect {
			handshakes = links.handshakes
		}
		var before uint64
		if handshakes != nil {
			before = handshakes()
		}
		marker := byte(90)
		probe := func() {
			t.Helper()
			if a.supervisor.outputChoices()["peer"] != path {
				t.Fatalf("packet %d: after revision %d expected path %v, observed %v", marker, f.verified.Cursor().Revision.Network, path, a.supervisor.outputChoices()["peer"])
			}
			exchange(b, "100.70.0.2", marker)
			marker += 2
		}
		for step := 0; step < 3; step++ {
			switch step {
			case 0:
				f.snapshot.Peers = append(f.snapshot.Peers, observer)
			case 1:
				f.snapshot.Peers[2].Endpoint = "192.0.2.78:51820"
				f.snapshot.Peers[2].EndpointGeneration++
			case 2:
				f.snapshot.Peers = f.snapshot.Peers[:2]
			}
			old := f.verified.Cursor()
			f.snapshot.Revision.Network++
			f.snapshot.Network.Revision++
			f.clock.advance(time.Millisecond, time.Millisecond)
			f.verify(t, &old)
			epoch := f.lease.MapEpoch(f.verified)
			probe()
			f.lease.PauseForApply(f.verified)
			if err := f.lease.Confirm(f.verified); err != nil {
				t.Fatal(err)
			}
			probe()
			if err := a.packets.Update(f.verified); err != nil {
				t.Fatal(err)
			}
			if err := a.supervisor.primary.Apply(t.Context(), f.verified, f.profile.Keys.WireGuardPrivate); err != nil {
				t.Fatal(err)
			}
			a.supervisor.Update(f.verified)
			if err := f.lease.EndSourceTransition(epoch); err != nil {
				t.Fatal(err)
			}
			f.lease.PausePackets(false)
			wait(func() bool {
				a.supervisor.mu.Lock()
				defer a.supervisor.mu.Unlock()
				return a.supervisor.statusCursor.Cursor() == f.verified.Cursor()
			})
			probe()
		}
		if handshakes != nil && handshakes() != before {
			t.Fatalf("path %v exchanged handshake messages during map application", path)
		}
		if path == peerRouteDirect && ab != checkSelectedPeerEngine(t, a.packets, "node", "peer", offerB, hubWG.expected) {
			t.Fatal("independent Direct engine replaced")
		}
	}
	var nativeApp *net.UDPConn
	if links.setupHub != nil {
		nativeApp = sendNativeRoutedUDP(t, b.tun)
	} else {
		exchange(b, "100.70.0.2", 10)
		checkChurn(peerRouteDirect)
	}
	exchange(c, "100.70.0.3", 20)
	links.disconnectFirst()
	var exchanges int
	wait(func() bool {
		if a.supervisor.outputChoices()["third"] != peerRouteDirect || c.supervisor.outputChoices()["node"] != peerRouteDirect {
			t.Fatal("unaffected Direct path lost")
		}
		exchange(c, "100.70.0.3", byte(exchanges))
		exchanges++
		return a.supervisor.outputChoices()["peer"] == peerRouteRelay && b.supervisor.outputChoices()["node"] == peerRouteRelay
	})
	if nativeApp != nil {
		select {
		case <-ab.done:
		case <-time.After(6 * time.Second):
			t.Fatal("WireGuardNT candidate was not retired")
		}
		hostRoute.verifyFallback(nativeApp)
	}
	exchange(b, "100.70.0.2", 40)
	if links.setupHub == nil {
		checkChurn(peerRouteRelay)
	}
	recipients := fixtures[:2]
	if links.setupHub != nil {
		recipients = fixtures
	}
	for i, f := range recipients {
		previous := f.verified.Cursor()
		if i < 2 {
			f.snapshot.Peers = f.snapshot.Peers[1:]
		}
		f.snapshot.Network.Revision++
		f.snapshot.Revision.Network++
		f.clock.advance(time.Millisecond, time.Millisecond)
		f.verify(t, &previous)
		epoch := f.lease.MapEpoch(f.verified)
		f.lease.PauseForApply(f.verified)
		if err := f.lease.Confirm(f.verified); err != nil {
			t.Fatal(err)
		}
		n := nodes[f.snapshot.Node.ID]
		if err := n.packets.Update(f.verified); err != nil {
			t.Fatal(err)
		}
		if n == a && hostRoute.refresh != nil {
			hostRoute.refresh(f.verified)
		}
		n.supervisor.Update(f.verified)
		if err := f.lease.EndSourceTransition(epoch); err != nil {
			t.Fatal(err)
		}
		f.lease.PausePackets(false)
	}
	wait(func() bool {
		return a.supervisor.outputChoices()["third"] == peerRouteDirect && c.supervisor.outputChoices()["node"] == peerRouteDirect && len(a.supervisor.transportStatuses(fixtures[0].verified)) == 1
	})
	if a.packets.send(cipherIPv4("100.70.0.1", "100.70.0.2", 50)) != ErrNoAuthority || b.packets.send(cipherIPv4("100.70.0.2", "100.70.0.1", 51)) != ErrNoAuthority {
		t.Fatal("revoked pair still admitted")
	}
	exchange(c, "100.70.0.3", 60)
	if ac != checkSelectedPeerEngine(t, a.packets, "node", "third", offerC, hubAWG) || ca != checkSelectedPeerEngine(t, c.packets, "third", "node", offerC, thirdBackend.expected) {
		t.Fatal("unaffected pair replaced its authenticated channel")
	}
	t.Logf("A-C completed %d bidirectional exchanges during A-B recovery and retained both channels after revocation", exchanges)
}

type threePeerLinks struct {
	setupHub        func(*testing.T, *candidateFixture, *supervisorConfig) searchRouteFixture
	listen          func(netip.AddrPort, string) (candidateSocket, error)
	handshakes      func() uint64
	disconnectFirst func()
}

func memoryThreePeerLinks() threePeerLinks {
	var sockets sync.Map
	var dropFirst atomic.Bool
	var wgHandshakes atomic.Uint64
	return threePeerLinks{
		handshakes:      wgHandshakes.Load,
		disconnectFirst: func() { dropFirst.Store(true) },
		listen: func(addr netip.AddrPort, profile string) (candidateSocket, error) {
			socket := &searchSocket{newCandidateSocketFixture(), addr}
			socket.forward = func(p candidateDatagram) error {
				if profile != "obfuscated" && wireGuardHandshakePacket(p.raw) {
					wgHandshakes.Add(1)
				}
				if dropFirst.Load() && profile != "obfuscated" {
					return nil
				}
				if target, ok := sockets.Load(p.address); ok {
					select {
					case target.(*searchSocket).reads <- candidateDatagram{p.raw, addr}:
					default:
					}
				}
				return nil
			}
			sockets.Store(addr, socket)
			return socket, nil
		},
	}
}
