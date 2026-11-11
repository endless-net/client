package dataplane

import (
	"bytes"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func qualifiedNegotiation(t *testing.T) (*candidateNegotiation, *candidateFixture) {
	t.Helper()
	n, f := negotiationFixture(t, false)
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	for trial := 0; trial < 3; trial++ {
		if trial > 0 {
			f.clock.advance(stableProbeSpacing, stableProbeSpacing)
			if err := n.Tick(); err != nil {
				t.Fatal(err)
			}
		}
		if err := n.Handle(replyControl(*n.prepare, transport.Prepared)); err != nil {
			t.Fatal(err)
		}
		completeNegotiatedProbe(t, n)
	}
	if n.commit == nil {
		t.Fatal("fixture not locally qualified")
	}
	return n, f
}

func activatedNegotiation(t *testing.T) (*candidateNegotiation, *candidateFixture, *directActivation) {
	t.Helper()
	n, f := qualifiedNegotiation(t)
	if err := n.Handle(replyControl(*n.commit, transport.Committed)); err != nil {
		t.Fatal(err)
	}
	token, err := n.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.Activate(token); err != nil {
		t.Fatal(err)
	}
	return n, f, token
}

func TestConfirmedDirectOutlivesSearchButNeverMapProfileOrLease(t *testing.T) {
	n, f, token := activatedNegotiation(t)
	if err := n.direct.Activate(token); err == nil {
		t.Fatal("activation token reused")
	}
	n.cycle.mu.Lock()
	n.cycle.wire.used = transport.MaxProbeBytes
	n.cycle.mu.Unlock()
	if err := n.cycle.charge(1); err != ErrProbeBudget {
		t.Fatal("fixture candidate budget not exhausted", err)
	}
	n.cycle.cancel()
	f.clock.advance(31*time.Second, 31*time.Second)
	endpoint, err := n.direct.bind.ParseEndpoint(n.assignment.offer.PeerEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.bind.Send([][]byte{{1, 2, 3}}, endpoint); err != nil {
		t.Fatal("confirmed encrypted traffic tied to expired search budget", err)
	}
	local, peer, err := n.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.QueueApplication(cipherIPv4(local.String(), peer.String(), 1)); err != nil {
		t.Fatal("confirmed app queue stopped with search", err)
	}
	if err := n.direct.QueueApplication(cipherIPv4("100.70.0.3", peer.String(), 1)); err == nil {
		t.Fatal("foreign plaintext source queued")
	}
	if err := n.direct.QueueApplication(cipherIPv4(local.String(), "100.70.0.3", 1)); err == nil {
		t.Fatal("another peer plaintext destination queued")
	}
	raw, err := controlPacket(local, peer, n.probe.request)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.QueueApplication(raw); err == nil {
		t.Fatal("app entrypoint forged internal control")
	}
	f.clock.advance(10*time.Second, 10*time.Second)
	if err := n.direct.bind.Send([][]byte{{4}}, endpoint); err == nil {
		t.Fatal("expired profile/map emitted ciphertext")
	}
	if err := n.direct.QueueApplication(cipherIPv4(local.String(), peer.String(), 2)); err == nil {
		t.Fatal("expired profile/map queued application")
	}
}

func TestLostACKRetainsQualifiedReceiverWithoutApplicationOutput(t *testing.T) {
	n, _ := qualifiedNegotiation(t)
	grant, err := n.TakeReceiveGrant()
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(grant), fmt.Sprintf("%#v", grant)} {
		if rendered != "[qualified Direct receive eligibility]" {
			t.Fatal("unsafe receiver grant formatting")
		}
	}
	if err := n.direct.AllowReceive(grant); err != nil {
		t.Fatal(err)
	}
	if err := n.direct.AllowReceive(grant); err == nil {
		t.Fatal("receiver grant reused")
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("lost ACK created outgoing selection")
	}
	local, peer, err := n.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.QueueApplication(cipherIPv4(local.String(), peer.String(), 1)); err == nil {
		t.Fatal("receive-only eligibility sent application")
	}
	n.cycle.cancel()
	n.direct.bind.mu.Lock()
	epoch := n.direct.bind.confirmationEpoch
	receive := n.direct.bind.receiveConfirmed
	n.direct.bind.mu.Unlock()
	if !receive {
		t.Fatal("lost ACK discarded receive eligibility")
	}
	if _, _, err := n.direct.bind.confirmedAdmission(netip.MustParseAddrPort(n.assignment.offer.PeerEndpoint), epoch); err != nil {
		t.Fatal("qualified receive admission stopped with search", err)
	}
	if _, _, err := n.direct.bind.confirmedAdmission(netip.MustParseAddrPort("192.0.2.30:51820"), epoch); err == nil {
		t.Fatal("qualified receiver admitted a foreign endpoint")
	}
	value := replyControl(n.probe.request, transport.Echo)
	value.Kind = transport.Challenge
	value.CandidateID = "2233445566778899aabbccddeeff0011"
	value.Nonce = bytes.Repeat([]byte{44}, 32)
	if err := n.direct.echoConfirmed(value); err != nil {
		t.Fatal("qualified receiver could not sustain fresh peer liveness", err)
	}
	if _, err := n.probe.Evidence(); err == nil {
		t.Fatal("receive eligibility revived expired own proof")
	}
}

func TestConfirmedDirectRenewalAndSemanticFences(t *testing.T) {
	n, f, _ := activatedNegotiation(t)
	epoch, err := n.direct.bind.transmitEpoch()
	if err != nil {
		t.Fatal(err)
	}
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if n.direct.bind.transmitCurrent(epoch) {
		t.Fatal("old accepted map remained current")
	}
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if !n.direct.bind.transmitCurrent(epoch) {
		t.Fatal("same-revision signature churned selected path")
	}
	cursor = f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.refresh(t, &cursor)
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if !n.direct.bind.transmitCurrent(epoch) {
		t.Fatal("unrelated revision invalidated an unchanged authorized path")
	}
	if _, err := n.direct.bind.transmitEpoch(); err != nil {
		t.Fatal("unchanged current channel unnecessarily deselected", err)
	}
	cursor = f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Peers[0].Transport.Profiles[0].PeerEndpoint = "192.0.2.21:51820"
	f.snapshot.Peers[0].Transport.PeerAdvertisement.Endpoints[0].Endpoint = "192.0.2.21:51820"
	f.refresh(t, &cursor)
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if _, err := n.direct.bind.transmitEpoch(); err == nil {
		t.Fatal("changed endpoint retained selected mode")
	}
}

func TestConfirmedDirectRechecksAfterBlockingDeadlineBoundary(t *testing.T) {
	n, f, _ := activatedNegotiation(t)
	socket := n.direct.bind.socket.(*candidateSocketFixture)
	select {
	case <-n.direct.bind.sendGate:
	case <-time.After(time.Second):
		t.Fatal("candidate writer did not yield fixture gate")
	}
	socket.mu.Lock()
	before := len(socket.writes)
	socket.beforeDeadline = func() { f.lease.Revoke() }
	socket.mu.Unlock()
	n.direct.bind.sendGate <- struct{}{}
	endpoint, err := n.direct.bind.ParseEndpoint(n.assignment.offer.PeerEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.direct.bind.Send([][]byte{{1}}, endpoint); err == nil {
		t.Fatal("revoked selected I/O accepted")
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if len(socket.writes) != before {
		t.Fatal("revoked ciphertext reached socket")
	}
}
