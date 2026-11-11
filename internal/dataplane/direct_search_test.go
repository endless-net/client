package dataplane

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relaywire "github.com/endless-net/relay/api/wire"
	signing "github.com/endless-net/signing/api/contract"
)

func reverseSearchFixture(t *testing.T, a *candidateFixture) *candidateFixture {
	t.Helper()
	var profile state.Profile
	raw, _ := json.Marshal(a.profile)
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	keys, err := state.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys.WireGuardPrivate = bytes.Repeat([]byte{84}, 32)
	profile.Keys = keys
	fingerprint, err := keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	profile.Attempt.DeviceKeyHash, profile.Attempt.NodeID = fingerprint, "peer"
	profile.Connection.Generation = 4
	var trust signing.ServerKeyResponse
	if err := json.Unmarshal(a.trust.Bundle, &trust); err != nil {
		t.Fatal(err)
	}
	scopes := []string{"node:register", "node:map", "node:endpoint"}
	payload, err := coordinator.NodeCredentialSigningPayload(trust.NodeCredentialTrustBundle.ActiveKeyID, "network", "peer", scopes, profile.Attempt.CredentialEnds)
	if err != nil {
		t.Fatal(err)
	}
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: trust.NodeCredentialTrustBundle.ActiveKeyID, NetworkID: "network", NodeID: "peer", Scopes: scopes, ExpiresAt: profile.Attempt.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{82}, 32)), payload))}
	raw, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	profile.Secrets.NodeCredential = "enc_" + base64.RawURLEncoding.EncodeToString(raw)
	b := &candidateFixture{clock: a.clock, profile: profile, mapKey: a.mapKey, trust: a.trust}
	local, remote := a.snapshot.Node, a.snapshot.Peers[0]
	localAd := remote.Transport.PeerAdvertisement
	node := coordinator.Node{ID: remote.ID, NetworkID: "network", PublicKey: remote.PublicKey, AssignedIP: "100.70.0.2", ApprovalState: "approved", Status: "online", EndpointExpiresAt: remote.EndpointExpiresAt, Transport: &localAd}
	peer := coordinator.Peer{ID: local.ID, NetworkID: "network", PublicKey: local.PublicKey, Endpoint: "192.0.2.10:51821", EndpointGeneration: 1, EndpointExpiresAt: local.EndpointExpiresAt, AllowedIPs: []string{"100.70.0.1/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.1/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}}
	peer.Transport = &coordinator.PeerTransportPlan{Version: coordinator.PeerTransportPlanVersion, Availability: remote.Transport.Availability, AccountID: "account", NetworkID: "network", LocalNodeID: node.ID, LocalPublicKey: node.PublicKey, PeerNodeID: peer.ID, PeerPublicKey: peer.PublicKey, LocalIntentGeneration: 4, Revision: a.snapshot.Revision, IssuedAt: remote.Transport.IssuedAt, ExpiresAt: remote.Transport.ExpiresAt, PeerAdvertisement: *local.Transport, Profiles: append([]coordinator.TransportProfileOffer(nil), remote.Transport.Profiles...)}
	for i := range peer.Transport.Profiles {
		offer := &peer.Transport.Profiles[i]
		offer.LocalEndpoint, offer.PeerEndpoint = offer.PeerEndpoint, offer.LocalEndpoint
	}
	b.snapshot = coordinator.NetworkMapSnapshot{Revision: a.snapshot.Revision, Network: a.snapshot.Network, Node: node, Peers: []coordinator.Peer{peer}}
	b.lease, err = connectivity.NewLease(t.Context(), b.clock, profile.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	b.refresh(t, nil)
	return b
}

type searchSocket struct {
	*candidateSocketFixture
	address netip.AddrPort
}

func (s *searchSocket) LocalAddr() net.Addr { return net.UDPAddrFromAddrPort(s.address) }

func TestTwoSupervisorsRecoverDirectWithActualAWGAfterFilteredWG(t *testing.T) {
	testTwoSupervisorsRecoverDirect(t, true, false, 0)
}

func TestTwoSupervisorsRecoverDirectWithActualWG(t *testing.T) {
	testTwoSupervisorsRecoverDirect(t, false, false, 0)
}

func TestTwoSupervisorsPreferRememberedAWGAndStillProbeFresh(t *testing.T) {
	testTwoSupervisorsRecoverDirect(t, false, true, 0)
}

func TestTwoSupervisorsRecoverAWGDuringGlobalRevisionChurn(t *testing.T) {
	testTwoSupervisorsRecoverDirect(t, true, false, 2*time.Second)
}

func TestTwoSupervisorsRecoverRelayAndAWGWhenRevisionFasterThanRTT(t *testing.T) {
	testTwoSupervisorsRecoverDirect(t, true, false, 100*time.Millisecond)
}

func testTwoSupervisorsRecoverDirect(t *testing.T, filterWG, preferAWG bool, revisionInterval time.Duration) {
	a := newCandidateFixture(t)
	caps := TransportCapabilities()
	a.snapshot.Node.Transport.Capabilities = caps
	a.snapshot.Node.Transport.Endpoints = []coordinator.TransportEndpoint{}
	a.snapshot.Peers[0].Transport.PeerAdvertisement.Capabilities = caps
	a.snapshot.Peers[0].Transport.PeerAdvertisement.Endpoints = []coordinator.TransportEndpoint{}
	profiles := []transport.Profile{cipherProfile(transport.WireGuard), cipherProfile(transport.AmneziaWG)}
	profiles[0].ID, profiles[1].ID = "plain", "obfuscated"
	var offers []coordinator.TransportProfileOffer
	for _, profile := range profiles {
		digest, err := transport.ProfileDigest(profile)
		if err != nil {
			t.Fatal(err)
		}
		offers = append(offers, coordinator.TransportProfileOffer{Profile: profile, Digest: digest})
	}
	a.snapshot.Peers[0].Transport.Profiles = offers
	previous := a.verified.Cursor()
	a.clock.advance(time.Millisecond, time.Millisecond)
	a.snapshot.Revision.Network++
	a.snapshot.Network.Revision++
	a.refresh(t, &previous)
	b := reverseSearchFixture(t, a)
	a.withRelay(t)
	b.withRelay(t)
	first, _, firstFrames, injectFirst := relayPipeFixtureFor(t, a)
	second, _, secondFrames, injectSecond := relayPipeFixtureFor(t, b)
	bridge := func(source *RelayTransport, frames <-chan *relaywire.ClientFrame, inject func(string, []byte), node string) {
		go func() {
			for {
				select {
				case <-source.done:
					return
				case <-t.Context().Done():
					return
				case frame := <-frames:
					if revisionInterval > 0 && revisionInterval < time.Second {
						time.Sleep(3 * revisionInterval / 2)
					}
					inject(node, frame.Payload)
				}
			}
		}()
	}
	bridge(first, firstFrames, injectSecond, "node")
	bridge(second, secondFrames, injectFirst, "peer")
	var networkMu sync.Mutex
	sockets := make(map[string]*searchSocket)
	var plainWrites, obfuscatedWrites int
	stores := make(map[string]*state.Store)
	contextDigest := strings.Repeat("9", 64)
	listen := func(node string) func(string, string, uint16) (candidateSocket, error) {
		return func(_, profile string, _ uint16) (candidateSocket, error) {
			address := netip.MustParseAddrPort("192.0.2.10:20001")
			other := "peer"
			if node == "peer" {
				address = netip.MustParseAddrPort("192.0.2.20:21001")
				other = "node"
			}
			if profile == "obfuscated" {
				address = netip.AddrPortFrom(address.Addr(), address.Port()+1)
			}
			socket := &searchSocket{newCandidateSocketFixture(), address}
			networkMu.Lock()
			sockets[node+"/"+profile] = socket
			networkMu.Unlock()
			inFlight := make(chan struct{}, portQueueFrames)
			socket.forward = func(packet candidateDatagram) error {
				networkMu.Lock()
				if profile == "plain" {
					plainWrites++
					if filterWG {
						networkMu.Unlock()
						return nil
					}
				} else {
					obfuscatedWrites++
				}
				networkMu.Unlock()
				deliver := func() {
					networkMu.Lock()
					target := sockets[other+"/"+profile]
					networkMu.Unlock()
					if target == nil || packet.address != target.address {
						return
					}
					select {
					case target.reads <- candidateDatagram{packet.raw, address}:
					case <-target.done:
					case <-t.Context().Done():
					default: // UDP receive-buffer overflow drops, never blocks Send.
					}
				}
				if revisionInterval > 0 && revisionInterval < time.Second {
					select {
					case inFlight <- struct{}{}:
					default:
						return nil
					}
					go func() {
						defer func() { <-inFlight }()
						timer := time.NewTimer(3 * revisionInterval / 2)
						defer timer.Stop()
						select {
						case <-timer.C:
							deliver()
						case <-socket.done:
						case <-t.Context().Done():
						}
					}()
				} else {
					deliver()
				}
				return nil
			}
			return socket, nil
		}
	}
	start := func(f *candidateFixture, connection *RelayTransport) (*transportSupervisor, *packetSwitch, *switchTUN) {
		store, _ := hintStoreFixture(t, f)
		stores[f.snapshot.Node.ID] = store
		if preferAWG || filterWG {
			sample, _ := f.clock.Sample()
			keys, err := hintKeys(f.verified, f.profile.ID, contextDigest, sample.Wall)
			if err != nil {
				t.Fatal(err)
			}
			v, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer clearHintStoreKeys(v)
			if err := store.Update(t.Context(), v.Revision, func(next *state.State) error {
				for _, key := range keys {
					if preferAWG && key.Engine == transport.AmneziaWG || filterWG && key.Engine == transport.WireGuard {
						h := state.TransportHint{Key: key, SucceededAt: sample.Wall, ExpiresAt: sample.Wall.Add(state.TransportHintTTL), StableSpan: 5 * time.Second, SuccessfulProbes: 3}
						return next.RecordTransportSuccess(h, sample.Wall)
					}
				}
				return state.ErrCorrupt
			}); err != nil {
				t.Fatal(err)
			}
		}
		packets, native := switchFixture(t, f)
		budget, err := newTransportBudget(1)
		if err != nil {
			t.Fatal(err)
		}
		slot, err := budget.reserve(f.lease, "", enginePrimary)
		if err != nil {
			t.Fatal(err)
		}
		primary, err := NewEngine(wireGuardPort{packets.primary}, &inertBind{}, 1, 51820, f.lease)
		if err != nil {
			t.Fatal(err)
		}
		primary.reservation = slot
		t.Cleanup(primary.Close)
		if err := primary.Apply(t.Context(), f.verified, f.profile.Keys.WireGuardPrivate); err != nil {
			t.Fatal(err)
		}
		g, err := newTransportSupervisor(t.Context(), f.verified, f.lease, f.clock, f.profile.ID, f.profile.Connection.Generation, f.profile.Keys.WireGuardPrivate, budget, packets, primary, supervisorConfig{relay: connection.config, probeInterval: time.Millisecond, retry: time.Second, mtu: 576, maxPeers: 1, listenCandidate: listen(f.snapshot.Node.ID), hintStore: store, networkContext: func(context.Context, []byte) (string, error) { return contextDigest, nil }}, func(context.Context, connectivity.Map, string) (*RelayTransport, error) { return connection, nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(g.Close)
		return g, packets, native
	}
	lastAdvance := time.Now()
	ga, pa, na := start(a, first)
	gb, pb, nb := start(b, second)
	lastAds := ""
	var lastMapAt time.Duration
	globalChanges := 0
	update := func() {
		adA := ga.advertisement(netip.MustParseAddr("192.0.2.10"))
		adB := gb.advertisement(netip.MustParseAddr("192.0.2.20"))
		raw, _ := json.Marshal([]coordinator.TransportAdvertisement{adA, adB})
		sample, _ := a.clock.Sample()
		renewal := revisionInterval > 0 && sample.Uptime-lastMapAt >= revisionInterval
		if string(raw) == lastAds && !renewal {
			return
		}
		if string(raw) == lastAds {
			globalChanges++
		}
		lastMapAt = sample.Uptime
		lastAds = string(raw)
		for i, f := range []*candidateFixture{a, b} {
			local, remote := adA, adB
			if i == 1 {
				local, remote = adB, adA
			}
			f.snapshot.Node.Transport = &local
			plan := f.snapshot.Peers[0].Transport
			plan.PeerAdvertisement = remote
			for j := range plan.Profiles {
				offer := &plan.Profiles[j]
				offer.LocalEndpoint, offer.PeerEndpoint = "", ""
				for _, ep := range local.Endpoints {
					if ep.ProfileID == offer.Profile.ID && ep.ProfileDigest == offer.Digest {
						offer.LocalEndpoint = ep.Endpoint
					}
				}
				for _, ep := range remote.Endpoints {
					if ep.ProfileID == offer.Profile.ID && ep.ProfileDigest == offer.Digest {
						offer.PeerEndpoint = ep.Endpoint
					}
				}
			}
			previous := f.verified.Cursor()
			f.snapshot.Revision.Network++
			f.snapshot.Network.Revision++
			sample, _ := f.clock.Sample()
			discoveryExpiry := sample.Wall.Add(time.Minute)
			f.snapshot.Node.EndpointExpiresAt = &discoveryExpiry
			f.snapshot.Peers[0].EndpointExpiresAt = &discoveryExpiry
			f.verify(t, &previous)
		}
		a.lease.PausePackets(true)
		b.lease.PausePackets(true)
		for _, f := range []*candidateFixture{a, b} {
			if err := f.lease.Confirm(f.verified); err != nil {
				t.Fatal(err)
			}
		}
		if err := pa.Update(a.verified); err != nil {
			t.Fatalf("native map update failed: %v Relay connected=%v/%v status=%v/%v", err, first.connected(), second.connected(), ga.transportStatuses(a.verified), gb.transportStatuses(b.verified))
		}
		if err := pb.Update(b.verified); err != nil {
			t.Fatal(err)
		}
		ga.Update(a.verified)
		gb.Update(b.verified)
		a.lease.PausePackets(false)
		b.lease.PausePackets(false)
	}
	// Drive wall/counter time together at real speed, including initial crypto
	// construction. The first initiation may arrive before the other peer bind;
	// the owner must retry within the same production recovery bound.
	readyDeadline := time.NewTimer(30 * time.Second)
	defer readyDeadline.Stop()
	readyTick := time.NewTicker(time.Millisecond)
	defer readyTick.Stop()
	stageStart, lastProgress := time.Now(), time.Now()
	resources := func(g *transportSupervisor) (int, int) {
		g.budget.mu.Lock()
		defer g.budget.mu.Unlock()
		return len(g.budget.engines), len(g.budget.probes)
	}
	for ga.outputChoices()["peer"] != peerRouteRelay || gb.outputChoices()["node"] != peerRouteRelay {
		now := time.Now()
		elapsed := now.Sub(lastAdvance)
		lastAdvance = now
		a.clock.advance(elapsed, elapsed)
		update()
		if time.Since(lastProgress) >= 5*time.Second {
			ea, pra := resources(ga)
			eb, prb := resources(gb)
			t.Logf("engines/probes=%d/%d %d/%d", ea, pra, eb, prb)
			t.Logf("waiting for initial Relay: connected=%v/%v status=%v/%v", first.connected(), second.connected(), ga.transportStatuses(a.verified), gb.transportStatuses(b.verified))
			lastProgress = time.Now()
		}
		ga.notify()
		gb.notify()
		select {
		case <-readyDeadline.C:
			t.Fatalf("initial authenticated Relay paths did not become ready: A=%v B=%v connected=%v/%v permissions=%v/%v", ga.transportStatuses(a.verified), gb.transportStatuses(b.verified), first.connected(), second.connected(), a.lease.Permissions(), b.lease.Permissions())
		case <-readyTick.C:
		}
	}
	t.Logf("own Relay proofs ready after %s", time.Since(stageStart))
	relayPayload := cipherIPv4("100.70.0.1", "100.70.0.2", 6)
	na.input <- relayPayload
	select {
	case got := <-nb.output:
		if !bytes.Equal(got, relayPayload) {
			t.Fatal("Relay payload changed during candidate preparation")
		}
	case <-time.After(time.Second):
		t.Fatal("background candidates interrupted the selected Relay")
	}
	lastAdvance = time.Now()
	for i := 0; i < 2500; i++ {
		now := time.Now()
		elapsed := now.Sub(lastAdvance)
		lastAdvance = now
		a.clock.advance(elapsed, elapsed)
		update()
		if time.Since(lastProgress) >= 5*time.Second {
			t.Logf("waiting for Direct: connected=%v/%v status=%v/%v", first.connected(), second.connected(), ga.transportStatuses(a.verified), gb.transportStatuses(b.verified))
			ea, pra := resources(ga)
			eb, prb := resources(gb)
			networkMu.Lock()
			t.Logf("Direct engines/probes=%d/%d %d/%d writes=%d/%d", ea, pra, eb, prb, plainWrites, obfuscatedWrites)
			networkMu.Unlock()
			t.Logf("listeners=%d/%d", len(ga.advertisement(netip.MustParseAddr("192.0.2.10")).Endpoints), len(gb.advertisement(netip.MustParseAddr("192.0.2.20")).Endpoints))
			lastProgress = time.Now()
		}
		ga.notify()
		gb.notify()
		statusA, statusB := ga.transportStatuses(a.verified), gb.transportStatuses(b.verified)
		if ga.outputChoices()["peer"] == peerRouteDirect && gb.outputChoices()["node"] == peerRouteDirect && len(statusA) == 1 && len(statusB) == 1 && statusA[0].SelectedPath == clientv0.PathKind_PATH_KIND_DIRECT && statusB[0].SelectedPath == clientv0.PathKind_PATH_KIND_DIRECT {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ga.outputChoices()["peer"] != peerRouteDirect || gb.outputChoices()["node"] != peerRouteDirect {
		t.Fatalf("Direct recovery did not finish: choices A=%v B=%v ads=%s", ga.outputChoices(), gb.outputChoices(), lastAds)
	}
	if revisionInterval > 0 && globalChanges < 2 {
		t.Fatal("scenario did not exercise frequent unrelated map updates")
	}
	networkMu.Lock()
	filtered := plainWrites
	networkMu.Unlock()
	if filterWG && filtered == 0 {
		t.Fatal("plain WG was not attempted before AWG")
	}
	if preferAWG && filtered != 0 {
		t.Fatal("remembered AWG did not precede unfiltered WG")
	}
	for i, g := range []*transportSupervisor{ga, gb} {
		value := a.verified
		if i == 1 {
			value = b.verified
		}
		status := g.transportStatuses(value)
		engine, version, profile := transport.WireGuard, "1", "plain"
		if filterWG || preferAWG {
			engine, version, profile = transport.AmneziaWG, "3.1", "obfuscated"
		}
		if len(status) != 1 || status[0].SelectedPath != clientv0.PathKind_PATH_KIND_DIRECT || status[0].Engine != engine || status[0].ProtocolVersion != version || status[0].TransportProfileId != profile || status[0].CheckedAt == nil {
			t.Fatal("actual selected profile was not publicly observed", status)
		}
		status[0].Engine = "mutated by reader"
		if g.transportStatuses(value)[0].Engine != engine {
			t.Fatal("status reader mutated actor state")
		}
	}
	for _, f := range []*candidateFixture{a, b} {
		deadline := time.Now().Add(time.Second)
		for {
			v, err := stores[f.snapshot.Node.ID].Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			count, fresh := len(v.TransportHints), false
			if count == 1 {
				h := v.TransportHints[0]
				fresh = h.Key.PeerNodeID != f.snapshot.Node.ID && h.Key.ContextDigest == contextDigest && h.SuccessfulProbes >= 3 && h.StableSpan >= 5*time.Second && h.SucceededAt.After(f.profile.Connection.AcceptedAt.Add(5*time.Second))
			}
			clearHintStoreKeys(v)
			if fresh {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("selected fresh Direct success was not durably recorded", count)
			}
			time.Sleep(time.Millisecond)
		}
	}
	raw := cipherIPv4("100.70.0.1", "100.70.0.2", 9)
	na.input <- raw
	select {
	case got := <-nb.output:
		if !bytes.Equal(got, raw) || len(pa.primary.queue) != 0 {
			t.Fatal("selected Direct payload changed or duplicated through primary")
		}
	case <-time.After(time.Second):
		t.Fatal("selected AWG did not deliver native plaintext")
	}
	reverse := cipherIPv4("100.70.0.2", "100.70.0.1", 10)
	nb.input <- reverse
	select {
	case got := <-na.output:
		if !bytes.Equal(got, reverse) || len(pb.primary.queue) != 0 {
			t.Fatal("reverse Direct payload changed or duplicated")
		}
	case <-time.After(time.Second):
		t.Fatal("reverse selected AWG path failed")
	}
	// Cipher peer proof is verified above; native app ACL classification is
	// outside this memory TUN fixture and is not claimed by this test.
}

func TestSearchChildrenKeepOriginalCounterDeadlineAcrossProfiles(t *testing.T) {
	f := newCandidateFixture(t)
	parent, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "search")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	first, err := parent.child("first")
	if err != nil {
		t.Fatal(err)
	}
	defer first.cancel()
	f.clock.advance(29*time.Second, 29*time.Second)
	second, err := parent.child("second")
	if err != nil {
		t.Fatal(err)
	}
	defer second.cancel()
	probe, err := second.attempt("own-new-nonce")
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := probe.timer.Remaining()
	if err != nil || remaining > time.Second {
		t.Fatal("new profile renewed the original cycle", remaining, err)
	}
	f.clock.advance(time.Second, time.Second)
	if second.check() == nil || probe.check() == nil {
		t.Fatal("child or attempt survived original parent deadline")
	}
	for failures := uint32(1); failures < 12; failures++ {
		base := 15 * time.Second
		for i := uint32(1); i < failures && base < 300*time.Second; i++ {
			base = min(300*time.Second, 2*base)
		}
		lower := max(15*time.Second, base*4/5)
		for range 8 {
			delay := directRetryDelay(failures)
			if delay < lower || delay > base {
				t.Fatal("retry jitter escaped approved bounds", failures, delay)
			}
		}
	}
}

func TestMapPauseDuringCandidateOpenDoesNotExhaustProfile(t *testing.T) {
	f := newCandidateFixture(t)
	packets, _ := switchFixture(t, f)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := budget.newCycle(t.Context(), f.clock, f.profile.ID, 1, "search")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, private: f.profile.Keys.WireGuardPrivate, packets: packets, budget: budget, config: supervisorConfig{mtu: 576}}
	g.config.listenCandidate = func(_, _ string, _ uint16) (candidateSocket, error) {
		f.lease.PausePackets(true)
		return newCandidateSocketFixture(), nil
	}
	s := &directSearch{parent: parent, order: []string{"fixture-profile"}, tried: make(map[string]bool), seen: make(map[string]bool)}
	p := &supervisedRelay{scope: packetBinding{peerID: "peer"}, search: s}
	g.tickSearch(p)
	if p.search != s || s.candidate != nil || s.tried["fixture-profile"] || len(budget.engines) != 0 {
		t.Fatal("a policy pause consumed the untested profile or leaked its engine")
	}
	f.lease.PausePackets(false)
}

func TestCompletedSourcePauseDoesNotConsumeUntestedProfile(t *testing.T) {
	f := newCandidateFixture(t)
	packets, _ := switchFixture(t, f)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := budget.newCycle(t.Context(), f.clock, f.profile.ID, 1, "search")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, private: f.profile.Keys.WireGuardPrivate, packets: packets, budget: budget, config: supervisorConfig{mtu: 576}}
	g.config.listenCandidate = func(_, _ string, _ uint16) (candidateSocket, error) {
		f.lease.PausePackets(true)
		f.lease.PausePackets(false)
		return nil, ErrPacketIO
	}
	s := &directSearch{parent: parent, order: []string{"fixture-profile"}, tried: make(map[string]bool), seen: make(map[string]bool)}
	p := &supervisedRelay{scope: packetBinding{peerID: "peer"}, search: s}
	g.tickSearch(p)
	if p.search != s || s.candidate != nil || s.tried["fixture-profile"] || len(budget.engines) != 0 {
		t.Fatal("completed source handoff was misclassified as a protocol failure")
	}
}

func TestHintPriorityWaitCannotConsumeWholeDirectSearchCycle(t *testing.T) {
	f := newCandidateFixture(t)
	packets, _ := switchFixture(t, f)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := budget.newCycle(t.Context(), f.clock, f.profile.ID, 1, "search")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	wait, err := parent.attempt("hint-priority")
	if err != nil {
		t.Fatal(err)
	}
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, private: f.profile.Keys.WireGuardPrivate, packets: packets, budget: budget, hints: &transportHintRuntime{}, config: supervisorConfig{mtu: 576}}
	opened := false
	g.config.listenCandidate = func(_, _ string, _ uint16) (candidateSocket, error) {
		opened = true
		f.lease.PausePackets(true)
		return nil, ErrPacketIO
	}
	s := &directSearch{parent: parent, hintEpoch: 1, hintBudget: wait, order: []string{"fixture-profile"}, tried: make(map[string]bool), seen: make(map[string]bool)}
	p := &supervisedRelay{scope: packetBinding{peerID: "peer"}, search: s}
	g.tickSearch(p)
	if opened {
		t.Fatal("fresh priority lookup was not given its bounded wait")
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	g.tickSearch(p)
	if !opened || p.search != s || s.hintReady || s.tried["fixture-profile"] {
		t.Fatal("unavailable hint priority blocked independent fresh Direct probes")
	}
	f.lease.PausePackets(false)
	if remaining, err := parent.remaining(); err != nil || remaining > 27*time.Second {
		t.Fatal("hint wait renewed the original search budget", remaining, err)
	}
}

func TestMapPauseDoesNotDestroyRetainedDirectCandidate(t *testing.T) {
	f := newCandidateFixture(t)
	c, err := buildAssignedCipher(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 576, f.clock, func([]byte) error { return nil }, func(uint16) (candidateSocket, error) { return newCandidateSocketFixture(), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, lease: f.lease}
	p := &supervisedRelay{direct: c}
	f.lease.PausePackets(true)
	if g.updateDirect(p, f.verified) || p.direct != c {
		t.Fatal("temporary fence destroyed candidate")
	}
	f.lease.PausePackets(false)
	if !g.updateDirect(p, f.verified) || p.direct != c {
		t.Fatal("current assignment was not retried after resume")
	}
}

func TestFollowerAcceptsNextInitiatorCycleWithoutRenewingProfileAllowance(t *testing.T) {
	n, f := negotiationFixture(t, true)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	route.relayConfirmed = true // Authenticated-event fixture, not path evidence.
	parent, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "follower-parent")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	n.cycle.parent = parent
	if err := n.cycle.charge(100); err != nil {
		t.Fatal(err)
	}
	wire := n.cycle.wire
	oldProbe, err := n.direct.BeginProbe(n.cycle, n.cycle.id)
	if err != nil {
		t.Fatal(err)
	}
	packets, _ := switchFixture(t, f)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	s := &directSearch{parent: parent, child: n.cycle, candidate: n.direct, negotiation: n, tried: map[string]bool{n.direct.profileID: true}, seen: map[string]bool{n.cycle.id: true}, traffic: map[string]*probeWireBudget{n.direct.profileID: wire}}
	p := &supervisedRelay{channel: n.relay, route: route, scope: packetBinding{localID: "node", peerID: "aaa-peer"}, search: s}
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, profile: f.profile.ID, generation: 1, private: f.profile.Keys.WireGuardPrivate, packets: packets, budget: budget, config: supervisorConfig{mtu: 576, listenCandidate: func(string, string, uint16) (candidateSocket, error) { return newCandidateSocketFixture(), nil }}}
	defer g.releaseCandidate(p, false)
	f.clock.advance(15*time.Second, 15*time.Second)
	value := followerPrepare(t, n, 89)
	value.CandidateID = strings.Repeat("b", 32)
	g.handlePeerControl(p, value)
	if p.search != s || s.parent != parent || s.child == nil || s.child == n.cycle || s.child.id != value.CandidateID || !s.seen[value.CandidateID] || s.child.wire != wire || wire.consumed() <= 100 {
		t.Fatal("a new initiator cycle was rejected or renewed the follower allowance")
	}
	if oldProbe.attempt.check() == nil || len(s.negotiation.history.seen) != 0 {
		t.Fatal("new candidate inherited old nonce or qualification")
	}
	if remaining, err := parent.remaining(); err != nil || remaining > 15*time.Second {
		t.Fatal("peer CID renewed parent deadline", remaining, err)
	}
	current := s.child
	replay := value
	replay.CandidateID = n.cycle.id
	g.handlePeerControl(p, replay)
	if s.child != current {
		t.Fatal("replayed CID replaced a newer trial")
	}
	wire.mu.Lock()
	wire.used = transport.MaxProbeBytes
	wire.mu.Unlock()
	value.CandidateID = strings.Repeat("c", 32)
	g.handlePeerControl(p, value)
	if s.candidate != nil || s.seen[value.CandidateID] || wire.consumed() != transport.MaxProbeBytes || len(budget.engines) != 0 {
		t.Fatal("fresh peer CID reset exhausted bytes or allocated another engine")
	}
}

func TestSearchResumesUnfinishedLastProfileBeforeRepeatingFailedProfiles(t *testing.T) {
	n, f := negotiationFixture(t, false)
	plain := cipherProfile(transport.WireGuard)
	plain.ID = "plain-first"
	digest, err := transport.ProfileDigest(plain)
	if err != nil {
		t.Fatal(err)
	}
	capability := transport.Capability{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}
	f.snapshot.Node.Transport.Capabilities.Engines = append(f.snapshot.Node.Transport.Capabilities.Engines, capability)
	f.snapshot.Peers[0].Transport.PeerAdvertisement.Capabilities.Engines = append(f.snapshot.Peers[0].Transport.PeerAdvertisement.Capabilities.Engines, capability)
	f.snapshot.Peers[0].Transport.Profiles = append(f.snapshot.Peers[0].Transport.Profiles, coordinator.TransportProfileOffer{Profile: plain, Digest: digest})
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.refresh(t, &cursor)
	if err := n.relay.bind.mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	s := &directSearch{parent: n.cycle, child: n.cycle, candidate: n.direct, negotiation: n, order: []string{plain.ID, n.direct.profileID}, tried: map[string]bool{plain.ID: true, n.direct.profileID: true}}
	p := &supervisedRelay{channel: n.relay, route: route, scope: packetBinding{localID: "node", peerID: "peer"}, search: s}
	g := &transportSupervisor{ctx: t.Context(), current: f.verified, snapshot: f.snapshot, lease: f.lease, clock: f.clock, profile: f.profile.ID, generation: 1, budget: budget}
	g.finishSearch(p, true)
	f.clock.advance(15*time.Second, 15*time.Second)
	if err := g.startSearch(p); err != nil {
		t.Fatal(err)
	}
	defer g.finishSearch(p, false)
	if p.search.parent == n.cycle || len(p.search.order) != 2 || p.search.order[0] != n.direct.profileID || len(p.search.tried) != 0 {
		t.Fatal("unfinished last profile starved behind already failed profiles or inherited old trial state")
	}
}
