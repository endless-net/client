package dataplane

import (
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func routeRelayProof(t *testing.T, n *candidateNegotiation, f *candidateFixture) *relayHealthProbe {
	t.Helper()
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "2233445566778899aabbccddeeff0011")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	probe, err := n.relay.BeginHealth(cycle)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	if err := probe.receive(replyControl(probe.request, transport.Echo)); err != nil {
		t.Fatal(err)
	}
	return probe
}

func routedDirectFixture(t *testing.T) (*peerRoute, *candidateNegotiation, *candidateFixture, *directActivation) {
	t.Helper()
	n, f := qualifiedNegotiation(t)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := route.ConfirmRelay(routeRelayProof(t, n, f)); err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(replyControl(*n.commit, transport.Committed)); err != nil {
		t.Fatal(err)
	}
	token, err := n.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := route.SelectDirect(token); err != nil {
		t.Fatal(err)
	}
	return route, n, f, token
}

func TestPeerRouteRequiresPeerProofAndInvalidatesPreviousOutputReceipts(t *testing.T) {
	n, f := qualifiedNegotiation(t)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !n.relay.bind.mux.transport.Ready() {
		t.Fatal("fixture TLS not ready")
	}
	if route.Current() != peerRouteNone {
		t.Fatal("TLS Ready selected peer Relay")
	}
	local, peer, err := n.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	raw := cipherIPv4(local.String(), peer.String(), 1)
	if err := route.Send(raw); err == nil {
		t.Fatal("unconfirmed Relay transmitted app")
	}
	if err := route.ConfirmRelay(routeRelayProof(t, n, f)); err != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteRelay {
		t.Fatal("own-nonce Relay not selected")
	}
	epoch := route.epoch
	if err := route.Send(raw); err != nil {
		t.Fatal(err)
	}
	if err := route.CanImprove(); err != nil {
		t.Fatal("initial Relay delayed Direct search", err)
	}
	if err := n.Handle(replyControl(*n.commit, transport.Committed)); err != nil {
		t.Fatal(err)
	}
	token, err := n.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	if old, err := route.SelectDirect(token); err != nil || old != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteDirect || route.guard(peerRouteRelay, epoch) {
		t.Fatal("switch retained old Relay output receipt")
	}
	if err := route.Send(raw); err != nil {
		t.Fatal("confirmed Direct not used", err)
	}
	if err := route.Send(cipherIPv4("100.70.0.3", peer.String(), 1)); err == nil {
		t.Fatal("foreign app source passed route")
	}
	route.Invalidate()
	if route.Current() != peerRouteNone || route.Send(raw) != ErrClosed {
		t.Fatal("invalidated route remained live")
	}
}

func TestPeerRoutePartialMapHandoffDoesNotDiscardSelectedDirect(t *testing.T) {
	route, n, f, _ := routedDirectFixture(t)
	epoch, hold := route.epoch, route.hold
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.refresh(t, &cursor)
	if err := n.relay.bind.mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteNone || route.kind != peerRouteDirect || route.epoch != epoch || route.hold != hold {
		t.Fatal("partial Relay-before-Direct map handoff permanently changed selection")
	}
	if route.Send(cipherIPv4("100.70.0.1", "100.70.0.2", 1)) == nil {
		t.Fatal("partial handoff admitted stale Direct output")
	}
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteDirect || route.epoch != epoch || route.hold != hold {
		t.Fatal("unchanged current Direct did not resume with original selection receipt")
	}
	f.lease.Revoke()
	if route.Current() != peerRouteNone {
		t.Fatal("temporary handoff logic bypassed hard revocation")
	}
}

func TestPeerRouteHysteresisCountsOriginalReceiptAndFailureBypassesIt(t *testing.T) {
	route, _, f, token := routedDirectFixture(t)
	if err := route.CanImprove(); !errors.Is(err, ErrHysteresis) {
		t.Fatal("switch did not hold quality improvements", err)
	}
	if _, err := route.SelectDirect(token); !errors.Is(err, ErrHysteresis) {
		t.Fatal("improvement skipped hold", err)
	}
	f.clock.advance(29*time.Second, 29*time.Second)
	if err := route.CanImprove(); !errors.Is(err, ErrHysteresis) {
		t.Fatal("hold ended early or was renewed", err)
	}
	f.clock.advance(time.Second, time.Second)
	if err := route.CanImprove(); err != nil {
		t.Fatal("original 30 second hold did not expire", err)
	}
	// A fresh fixture keeps the separately bounded Relay bearer current.
	route, n, f, _ := routedDirectFixture(t)
	f.clock.advance(time.Second, time.Second)
	epoch := route.epoch
	route.FailDirect(n.direct)
	if route.Current() != peerRouteRelay || route.guard(peerRouteDirect, epoch) {
		t.Fatal("failed Direct blocked Relay fallback")
	}
	if err := route.CanImprove(); err != nil {
		t.Fatal("old hysteresis delayed recovery", err)
	}
	n.direct.bind.mu.Lock()
	receive, transmit := n.direct.bind.receiveConfirmed, n.direct.bind.transmitConfirmed
	n.direct.bind.mu.Unlock()
	if !receive || transmit {
		t.Fatal("fallback lost qualified receive eligibility or retained app output")
	}
	local, peer, err := n.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	if err := route.Send(cipherIPv4(local.String(), peer.String(), 1)); err != nil {
		t.Fatal("working Relay stopped after Direct failure", err)
	}
}

func TestPeerRouteRelayFailureDoesNotDestroyHealthyDirectAndNoProofResurrectsAfterRevoke(t *testing.T) {
	route, n, f, _ := routedDirectFixture(t)
	route.FailRelay(n.relay)
	if route.Current() != peerRouteDirect {
		t.Fatal("backup Relay failure destroyed current Direct")
	}
	route.FailDirect(n.direct)
	if route.Current() != peerRouteNone {
		t.Fatal("unconfirmed backup resurrected after both failures")
	}
	probe := routeRelayProof(t, n, f)
	f.lease.Revoke()
	if err := route.ConfirmRelay(probe); err == nil {
		t.Fatal("stale successful Relay proof restored revoked route")
	}
	local, peer, err := n.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	if err := route.Send(cipherIPv4(local.String(), peer.String(), 1)); err == nil {
		t.Fatal("revoked route sent application")
	}
}

func TestPeerRouteRejectsForeignRelayProofAndReplacement(t *testing.T) {
	n, f := qualifiedNegotiation(t)
	route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, foreign := qualifiedNegotiation(t)
	proof := routeRelayProof(t, other, foreign)
	if err := route.ConfirmRelay(proof); err == nil {
		t.Fatal("foreign channel proof selected Relay")
	}
	if err := route.ReplaceRelay(other.relay); err == nil {
		t.Fatal("foreign device key replaced retained peer Relay")
	}
	if err := route.ReplaceRelay(n.relay); err != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteNone {
		t.Fatal("replacement inherited peer health")
	}
	if err := route.ConfirmRelay(routeRelayProof(t, n, f)); err != nil {
		t.Fatal(err)
	}
	if route.Current() != peerRouteRelay {
		t.Fatal("replacement did not accept its own proof")
	}
}
