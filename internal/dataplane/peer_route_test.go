package dataplane

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

func TestAttachedAdapterCannotSendAcrossSocketReplacement(t *testing.T) {
	_, n, _, _ := routedDirectFixture(t)
	bind := n.direct.bind
	endpoint, err := bind.ParseEndpoint(n.assignment.peerEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	replacement := newCandidateSocketFixture()
	defer func() { _ = replacement.Close() }()
	bind.mu.Lock()
	original := bind.socket
	bind.adapterSocket = original
	bind.mu.Unlock()
	defer func() {
		bind.mu.Lock()
		bind.socket = original
		bind.mu.Unlock()
	}()
	packets := [][]byte{{1, 2, 3}}
	err = bind.send(packets, endpoint, func(int) error {
		bind.mu.Lock()
		bind.socket = replacement
		bind.mu.Unlock()
		return nil
	})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("socket replaced during send: %v", err)
	}
	if err := bind.send(packets, endpoint, nil); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("subsequent send on replaced socket: %v", err)
	}
	select {
	case <-replacement.wrote:
		t.Fatal("replacement socket received old adapter output")
	default:
	}
}

func TestConfirmedSendDropsCiphertextAcrossSourcePause(t *testing.T) {
	_, n, f, _ := routedDirectFixture(t)
	bind := n.direct.bind
	endpoint, err := bind.ParseEndpoint(n.assignment.peerEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	packets := [][]byte{{1, 2, 3}}
	err = bind.send(packets, endpoint, func(int) error {
		f.lease.PausePackets(true)
		f.lease.PausePackets(false)
		return nil
	})
	if !errors.Is(err, connectivity.ErrSourceChanged) {
		t.Fatalf("queued send after source pause: %v", err)
	}
	if err := bind.send(packets, endpoint, nil); err != nil {
		t.Fatalf("fresh send after source resume: %v", err)
	}
}

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
	if _, err := route.selectDirect(token, func() error { return ErrPolicyOwnership }); !errors.Is(err, ErrPolicyOwnership) {
		t.Fatalf("native policy rejection: %v", err)
	}
	if token.used.Load() || route.Current() != peerRouteRelay || !route.guard(peerRouteRelay, epoch) {
		t.Fatal("failure to enable the adapter consumed the token or invalidated Relay")
	}
	b := n.direct.bind
	b.mu.Lock()
	b.adapterSocket = b.socket
	b.mu.Unlock()
	if _, err := route.SelectDirect(token); !errors.Is(err, ErrNoAuthority) || token.used.Load() {
		t.Fatal("routed adapter activated without OS route commit", err)
	}
	b.mu.Lock()
	b.adapterSocket = nil
	b.mu.Unlock()
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

func TestPeerRouteFailedSelectionPreservesRelayAndActivation(t *testing.T) {
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
	epoch := route.epoch
	route.clock = &probeClock{err: errors.New("clock unavailable")}
	if _, err := route.SelectDirect(token); !errors.Is(err, connectivity.ErrClock) {
		t.Fatal("expected selection clock failure", err)
	}
	route.clock = f.clock
	if route.Current() != peerRouteRelay || route.epoch != epoch || token.used.Load() {
		t.Fatal("failed selection changed Relay or consumed the bilateral commit")
	}
	if _, err := n.direct.bind.transmitEpoch(); err == nil {
		t.Fatal("rejected candidate gained application output")
	}
	if _, err := route.SelectDirect(token); err != nil || route.Current() != peerRouteDirect {
		t.Fatal("could not retry the same bilateral commit", err)
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
