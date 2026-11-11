package dataplane

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func confirmedMeterFixture(t *testing.T) (*assignedCipher, *candidateFixture, *pathChallenge) {
	t.Helper()
	n, f, _ := activatedNegotiation(t)
	n.cycle.cancel()
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "33445566778899001122334455667788")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	p, err := n.direct.BeginProbe(cycle, cycle.id)
	if err != nil {
		t.Fatal(err)
	}
	return n.direct, f, p
}

func meterRaw(t *testing.T, c *assignedCipher, value transport.Control, incoming bool) []byte {
	t.Helper()
	a, _, _ := c.controlSnapshot()
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	if incoming {
		local, peer = peer, local
	}
	raw, err := controlPacket(local, peer, value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func peerMeterChallenge(p *pathChallenge) transport.Control {
	value := replyControl(p.request, transport.Challenge)
	value.Nonce = bytes.Repeat([]byte{41}, 32)
	return value
}

func TestConfirmedControlMetersActualWireBothWaysWithoutAppOrRekeyQuota(t *testing.T) {
	c, _, p := confirmedMeterFixture(t)
	guard := c.controlSendGuard(meterRaw(t, c, p.request, false))
	if guard == nil {
		t.Fatal("own control lost its annotation")
	}
	if err := guard(1801); err != nil {
		t.Fatal(err)
	}
	if got := p.attempt.cycle.wire.consumed(); got != 1801+28 {
		t.Fatal("outbound actual ciphertext length not charged", got)
	}
	if err := c.controlReceived(meterRaw(t, c, replyControl(p.request, transport.Echo), true), 1993); err != nil {
		t.Fatal(err)
	}
	want := 1801 + 28 + 1993 + 28
	if got := p.attempt.cycle.wire.consumed(); got != want {
		t.Fatal("inbound actual ciphertext length not charged", got)
	}
	if got := c.serviceWindow.wire.consumed(); got != want {
		t.Fatal("retained-channel window lost wire charge", got)
	}
	if _, err := p.Evidence(); err == nil {
		t.Fatal("wire metering created path evidence")
	}
	if err := p.attempt.cycle.charge(transport.MaxProbeBytes - want); err != nil {
		t.Fatal(err)
	}
	if err := guard(73); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("confirmed own proof bypassed exhausted shared search ledger", err)
	}
	a, _, _ := c.controlSnapshot()
	local, peer, _ := a.addresses()
	app := cipherIPv4(local.String(), peer.String(), 8)
	if c.controlSendGuard(app) != nil || c.controlReceived(cipherIPv4(peer.String(), local.String(), 9), 91) != nil {
		t.Fatal("application inherited service quota")
	}
	endpoint, _ := c.bind.ParseEndpoint(a.offer.PeerEndpoint)
	// Crypto maintenance bypasses service hooks and still needs current channel
	// admission. It must not inherit an exhausted search or service window.
	if err := c.bind.Send([][]byte{{1, 2, 3}}, endpoint); err != nil {
		t.Fatal("unannotated authorized ciphertext stopped with probe quota", err)
	}
	if err := c.QueueApplication(app); err != nil {
		t.Fatal("authorized application queue stopped with probe quota", err)
	}
}

func TestConfirmedServiceGuardRetainsOriginalNonceDeadlineAndCancellation(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "replace"} {
		t.Run(mode, func(t *testing.T) {
			c, f, p := confirmedMeterFixture(t)
			guard := c.controlSendGuard(meterRaw(t, c, p.request, false))
			switch mode {
			case "timeout":
				f.clock.advance(3100*time.Millisecond, 3100*time.Millisecond)
			case "cancel":
				p.attempt.cancel()
			case "replace":
				if _, err := c.BeginProbe(p.attempt.cycle, p.request.CandidateID); err != nil {
					t.Fatal(err)
				}
			}
			if err := guard(400); err == nil {
				t.Fatal("late encrypted control inherited a new probe's authority")
			}
			if got := p.attempt.cycle.wire.consumed(); got != 0 {
				t.Fatal("rejected queued control consumed another probe's wire allowance", got)
			}
		})
	}
}

func TestPeerEchoReceiptDuplicatesKeepThreeSecondsAndCannotConfirmOwnProbe(t *testing.T) {
	c, f, p := confirmedMeterFixture(t)
	challenge := peerMeterChallenge(p)
	incoming := meterRaw(t, c, challenge, true)
	if err := c.controlReceived(incoming, 2000); err != nil {
		t.Fatal(err)
	}
	guard := c.controlSendGuard(meterRaw(t, c, replyControl(challenge, transport.Echo), false))
	if guard == nil || guard(3000) != nil {
		t.Fatal("fresh authenticated peer challenge could not echo")
	}
	window := c.serviceWindow
	if got := window.wire.consumed(); got != 2000+28+3000+28 {
		t.Fatal("peer challenge/echo escaped actual wire budget", got)
	}
	f.clock.advance(2900*time.Millisecond, 2900*time.Millisecond)
	if err := c.controlReceived(incoming, 2100); err != nil {
		t.Fatal("in-window duplicate rejected before original receipt expiry", err)
	}
	f.clock.advance(200*time.Millisecond, 200*time.Millisecond)
	if err := c.controlReceived(incoming, 2200); err == nil {
		t.Fatal("duplicate peer challenge renewed its three-second receipt")
	}
	if err := guard(3100); err == nil {
		t.Fatal("queued Echo outlived its first authenticated receipt")
	}
	if _, err := p.Evidence(); err == nil {
		t.Fatal("peer challenge/echo created own Direct proof")
	}
	if c.serviceWindow != window {
		t.Fatal("duplicate renewed the maintenance window")
	}
}

func TestConfirmedServiceWindowSurvivesNewCandidateAndExhaustion(t *testing.T) {
	c, f, p := confirmedMeterFixture(t)
	challenge := peerMeterChallenge(p)
	if err := c.controlReceived(meterRaw(t, c, challenge, true), 1500); err != nil {
		t.Fatal(err)
	}
	window := c.serviceWindow
	if err := window.charge(transport.MaxProbeBytes - window.wire.consumed()); err != nil {
		t.Fatal(err)
	}
	p.attempt.cycle.cancel()
	challenge.CandidateID = "44556677889900112233445566778899"
	challenge.Nonce = bytes.Repeat([]byte{42}, 32)
	if err := c.controlReceived(meterRaw(t, c, challenge, true), 1501); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("new peer CID bypassed exhausted retained-channel quota", err)
	}
	if c.serviceWindow != window {
		t.Fatal("new peer CID renewed maintenance bytes/deadline")
	}
	// Ordinary data keeps working even after the search is over and both probe
	// quotas are exhausted; only original map/profile/lease still authorize it.
	a, _, _ := c.controlSnapshot()
	endpoint, _ := c.bind.ParseEndpoint(a.offer.PeerEndpoint)
	if err := c.bind.Send([][]byte{{4}}, endpoint); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(30*time.Second, 30*time.Second)
	challenge.Nonce = bytes.Repeat([]byte{43}, 32)
	if err := c.controlReceived(meterRaw(t, c, challenge, true), 1502); err != nil {
		t.Fatal("original thirty-second window never allowed later maintenance", err)
	}
	if c.serviceWindow == window || c.serviceWindow.wire.consumed() != 1502+28 {
		t.Fatal("new maintenance window did not start from its own receipt")
	}
	if !errors.Is(window.check(), context.DeadlineExceeded) {
		t.Fatal("old service window resurrected")
	}
}

func TestServiceMetadataCannotOutliveSemanticEndpointChange(t *testing.T) {
	c, f, p := confirmedMeterFixture(t)
	own := c.controlSendGuard(meterRaw(t, c, p.request, false))
	challenge := peerMeterChallenge(p)
	if err := c.controlReceived(meterRaw(t, c, challenge, true), 2000); err != nil {
		t.Fatal(err)
	}
	echo := c.controlSendGuard(meterRaw(t, c, replyControl(challenge, transport.Echo), false))
	window := c.serviceWindow
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].Transport.Profiles[0].PeerEndpoint = "192.0.2.21:51820"
	f.snapshot.Peers[0].Transport.PeerAdvertisement.Endpoints[0].Endpoint = "192.0.2.21:51820"
	f.refresh(t, &cursor)
	if err := c.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if own(99) == nil || echo(99) == nil {
		t.Fatal("queued service metadata survived semantic endpoint change")
	}
	if c.serviceWindow != window {
		t.Fatal("map adoption replenished service bytes/deadline")
	}
}

func TestServiceGuardNarrowsPhysicalWriteDeadlineAndNeverWritesAfterQuota(t *testing.T) {
	c, f, p := confirmedMeterFixture(t)
	guard := c.controlSendGuard(meterRaw(t, c, p.request, false))
	f.clock.advance(2*time.Second, 2*time.Second)
	endpoint, err := c.bind.ParseEndpoint(p.assignment.offer.PeerEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	socket := c.bind.socket.(*candidateSocketFixture)
	if err := c.bind.SendGuarded([][]byte{bytes.Repeat([]byte{8}, 317)}, endpoint, []func(int) error{guard}); err != nil {
		t.Fatal(err)
	}
	sample, _ := f.clock.Sample()
	socket.mu.Lock()
	deadline, count := socket.deadline, len(socket.writes)
	socket.mu.Unlock()
	if deadline.After(sample.Wall.Add(time.Second)) {
		t.Fatal("specific original probe deadline replaced by fresh three seconds", deadline, sample.Wall)
	}
	window := c.serviceWindow
	if err := window.charge(transport.MaxProbeBytes - window.wire.consumed()); err != nil {
		t.Fatal(err)
	}
	if err := c.bind.SendGuarded([][]byte{{3, 4}}, endpoint, []func(int) error{guard}); !errors.Is(err, ErrProbeBudget) {
		t.Fatal("exhausted service budget reached physical sender", err)
	}
	socket.mu.Lock()
	after := len(socket.writes)
	socket.mu.Unlock()
	if after != count {
		t.Fatal("quota-rejected service ciphertext reached socket")
	}
	if err := c.bind.Send([][]byte{{5}}, endpoint); err != nil {
		t.Fatal("probe quota stopped ordinary ciphertext", err)
	}
}

func TestServiceWindowClockFailureCannotBeRenewed(t *testing.T) {
	c, f, p := confirmedMeterFixture(t)
	challenge := peerMeterChallenge(p)
	if err := c.controlReceived(meterRaw(t, c, challenge, true), 1000); err != nil {
		t.Fatal(err)
	}
	window := c.serviceWindow
	f.clock.advance(-time.Millisecond, time.Millisecond)
	if err := c.chargeService(999, nil); err == nil {
		t.Fatal("clock rollback authorized service bytes")
	}
	f.clock.advance(31*time.Second, 31*time.Second)
	if err := c.chargeService(999, nil); err == nil || c.serviceWindow != window {
		t.Fatal("unsafe clock revived service receipt/window", err)
	}
}

func TestSupervisorLocalServiceQuotaKeepsSelectedDirectAndOldProofTime(t *testing.T) {
	route, n, f, _ := routedDirectFixture(t)
	n.cycle.cancel()
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "55667788990011223344556677889900")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	probe, err := n.direct.BeginProbe(cycle, cycle.id)
	if err != nil {
		t.Fatal(err)
	}
	guard := n.direct.controlSendGuard(meterRaw(t, n.direct, probe.request, false))
	if err := guard(1000); err != nil {
		t.Fatal(err)
	}
	window := n.direct.serviceWindow
	if err := window.charge(transport.MaxProbeBytes - window.wire.consumed()); err != nil {
		t.Fatal(err)
	}
	if err := guard(1001); !errors.Is(err, ErrProbeBudget) {
		t.Fatal(err)
	}
	if !errors.Is(probe.attempt.check(), ErrProbeBudget) {
		t.Fatal("asynchronous packet guard failure did not reach the actor")
	}
	oldProof, _ := f.clock.Sample()
	f.clock.advance(time.Millisecond, time.Millisecond)
	now, _ := f.clock.Sample()
	p := &supervisedRelay{direct: n.direct, route: route, directHealth: probe, directCycle: cycle, checkedDirect: oldProof}
	g := &transportSupervisor{config: supervisorConfig{probeInterval: time.Second}}
	g.healthDirect(p, now)
	if p.directHealth != nil || route.Current() != peerRouteDirect || p.checkedDirect != oldProof {
		t.Fatal("local quota evicted working Direct or refreshed its proof time")
	}
	f.clock.advance(2*time.Second, 2*time.Second)
	now, _ = f.clock.Sample()
	g.healthDirect(p, now) // Empty budget must not construct another own probe.
	if p.directHealth != nil || route.Current() != peerRouteDirect || n.direct.serviceWindow != window {
		t.Fatal("quota cooldown resumed probes early or reset the window")
	}
}

// This test isolates retained-channel metering with real pinned crypto. Receive/
// transmit eligibility is a deliberate unit seam after the cold own Echo; full
// three-proof bilateral activation is covered by the two-supervisor tests.
func TestRetainedControlMeterWithActualPinnedCryptoAndApplicationTraffic(t *testing.T) {
	for _, engine := range []string{transport.WireGuard, transport.AmneziaWG} {
		t.Run(engine, func(t *testing.T) {
			a := newCandidateFixture(t)
			profile := cipherProfile(engine)
			if profile.AWG != nil {
				profile.AWG.ContentPadding = transport.Range{} // Dynamic random trailers, S4 retained.
			}
			digest, err := transport.ProfileDigest(profile)
			if err != nil {
				t.Fatal(err)
			}
			plan := a.snapshot.Peers[0].Transport
			plan.Profiles[0].Profile, plan.Profiles[0].Digest = profile, digest
			caps := transport.Capabilities{Version: 1, Engines: []transport.Capability{{Engine: engine, ProtocolVersion: profile.ProtocolVersion, ProfileVersion: 1}}}
			a.snapshot.Node.Transport.Capabilities, plan.PeerAdvertisement.Capabilities = caps, caps
			a.snapshot.Node.Transport.Endpoints[0].ProfileDigest = digest
			plan.PeerAdvertisement.Endpoints[0].ProfileDigest = digest
			cursor := a.verified.Cursor()
			a.clock.advance(time.Millisecond, time.Millisecond)
			a.snapshot.Revision.Network++
			a.snapshot.Network.Revision++
			a.refresh(t, &cursor)
			b := reverseSearchFixture(t, a)
			fixtures := []*candidateFixture{a, b}
			sockets := []*searchSocket{{newCandidateSocketFixture(), netip.MustParseAddrPort("192.0.2.10:51821")}, {newCandidateSocketFixture(), netip.MustParseAddrPort("192.0.2.20:51820")}}
			for i, socket := range sockets {
				target := sockets[1-i]
				socket.forward = func(packet candidateDatagram) error {
					select {
					case target.reads <- candidateDatagram{packet.raw, socket.address}:
					case <-target.done:
					default:
					}
					return nil
				}
			}
			channels := make([]*assignedCipher, 2)
			cycles := make([]*probeCycle, 2)
			proofs := make([]*pathChallenge, 2)
			output := make(chan []byte, 4)
			const cid = "66778899001122334455667788990011"
			for i, f := range fixtures {
				socket := sockets[i]
				peer := f.snapshot.Peers[0].ID
				channels[i], err = buildAssignedCipher(t.Context(), f.verified, f.lease, peer, profile.ID, f.profile.Keys.WireGuardPrivate, 1420, f.clock, func(raw []byte) error { output <- bytes.Clone(raw); return nil }, func(uint16) (candidateSocket, error) { return socket, nil })
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(channels[i].Close)
				cycles[i], err = newProbeCycle(t.Context(), f.clock, f.profile.ID, f.profile.Connection.Generation, cid)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(cycles[i].cancel)
				proofs[i], err = channels[i].BeginProbe(cycles[i], cid)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := proofs[0].Send(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-proofs[0].done:
			case <-time.After(2 * time.Second):
				t.Fatal("actual cold crypto did not return own Echo")
			}
			for i, c := range channels {
				cycles[i].cancel()
				c.bind.mu.Lock()
				c.bind.receiveConfirmed, c.bind.transmitConfirmed = true, true
				c.bind.mu.Unlock()
			}
			fresh, err := newProbeCycle(t.Context(), a.clock, a.profile.ID, 1, "77889900112233445566778899001122")
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.cancel()
			proof, err := channels[0].BeginProbe(fresh, fresh.id)
			if err != nil {
				t.Fatal(err)
			}
			if err := proof.Send(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-proof.done:
			case <-time.After(time.Second):
				t.Fatal("retained actual crypto did not return own Echo")
			}
			if _, err := proof.Evidence(); err != nil {
				t.Fatal(err)
			}
			consumed := fresh.wire.consumed()
			plain := len(meterRaw(t, channels[0], proof.request, false))
			if consumed <= 2*(plain+28) || consumed != channels[0].serviceWindow.wire.consumed() || consumed != channels[1].serviceWindow.wire.consumed() {
				t.Fatal("actual bidirectional ciphertext size lost between supplier hooks and service ledger", consumed, plain, channels[0].serviceWindow.wire.consumed(), channels[1].serviceWindow.wire.consumed())
			}
			app := cipherIPv4("100.70.0.1", "100.70.0.2", 19)
			if err := channels[0].QueueApplication(app); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-output:
				if !bytes.Equal(got, app) {
					t.Fatal("control leaked into application output")
				}
			case <-time.After(time.Second):
				t.Fatal("actual app ciphertext did not cross retained channel")
			}
			if fresh.wire.consumed() != consumed || channels[0].serviceWindow.wire.consumed() != consumed || channels[1].serviceWindow.wire.consumed() != consumed {
				t.Fatal("application wire consumed probe/service allowance")
			}
			if err := channels[0].serviceWindow.charge(transport.MaxProbeBytes - consumed); err != nil {
				t.Fatal(err)
			}
			limited, err := channels[0].BeginProbe(fresh, fresh.id)
			if err != nil {
				t.Fatal(err)
			}
			if err := limited.Send(); err != nil && !errors.Is(err, ErrProbeBudget) {
				t.Fatal(err)
			}
			select {
			case <-limited.attempt.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("actual encrypted send did not report local quota exhaustion")
			}
			if !errors.Is(limited.attempt.check(), ErrProbeBudget) {
				t.Fatal("quota rejection looked like a remote path timeout")
			}
			app = cipherIPv4("100.70.0.1", "100.70.0.2", 20)
			if err := channels[0].QueueApplication(app); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-output:
				if !bytes.Equal(got, app) {
					t.Fatal("quota-rejected control leaked to app or duplicated a packet")
				}
			case <-time.After(time.Second):
				t.Fatal("probe quota interrupted actual retained application ciphertext")
			}
		})
	}
}
