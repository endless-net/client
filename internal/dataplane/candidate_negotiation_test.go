package dataplane

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

// Protocol-state fixtures inject authenticated-event/own-echo inputs. Actual
// engine/source/Relay frame authentication is covered by the crypto fixtures;
// these tests do not assert external network reachability or native app ACL.
func negotiationFixture(t *testing.T, follower bool) (*candidateNegotiation, *candidateFixture) {
	t.Helper()
	mux, f, _, _ := relayMuxFixture(t)
	peerID := "peer"
	if follower {
		peerID = "aaa-peer"
		f.snapshot.Peers[0].ID = peerID
		f.snapshot.Peers[0].Transport.PeerNodeID = peerID
		cursor := f.verified.Cursor()
		f.clock.advance(time.Millisecond, time.Millisecond)
		f.refresh(t, &cursor)
		if err := mux.Update(f.verified); err != nil {
			t.Fatal(err)
		}
	}
	relay, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, peerID, f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Close)
	socket := newCandidateSocketFixture()
	direct, err := buildAssignedCipher(t.Context(), f.verified, f.lease, peerID, "fixture-profile", f.profile.Keys.WireGuardPrivate, 1280, f.clock, func([]byte) error { return nil }, func(uint16) (candidateSocket, error) { return socket, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(direct.Close)
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "112233445566778899aabbccddeeff00")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	negotiation, err := newCandidateNegotiation(direct, relay, cycle)
	if err != nil {
		t.Fatal(err)
	}
	return negotiation, f
}

func completeNegotiatedProbe(t *testing.T, n *candidateNegotiation) {
	t.Helper()
	if n.probe == nil {
		t.Fatal("probe not prepared")
	}
	if !n.probe.hasSent() {
		challenge := replyControl(n.probe.request, transport.Challenge)
		challenge.Nonce = bytes.Repeat([]byte{79}, 32)
		if err := n.probe.receive(challenge); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.probe.receive(replyControl(n.probe.request, transport.Echo)); err != nil {
		t.Fatal(err)
	}
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
}

func followerPrepare(t *testing.T, n *candidateNegotiation, nonce byte) transport.Control {
	t.Helper()
	local, err := n.relay.bind.newControl(transport.Prepared, n.assignment.offer.Digest, n.cycle.id, bytes.Repeat([]byte{nonce}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return replyControl(local, transport.Prepare)
}

func TestFollowerRetriesPrepareAfterTemporaryProbeCapacity(t *testing.T) {
	n, _ := negotiationFixture(t, true)
	budget, err := newTransportBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	n.cycle.budget = budget
	var permits []*probePermit
	for _, id := range []string{"busy-one", "busy-two"} {
		attempt, err := n.cycle.attempt(id)
		if err != nil {
			t.Fatal(err)
		}
		permit, err := budget.tryProbe(attempt)
		if err != nil {
			t.Fatal(err)
		}
		permits = append(permits, permit)
		defer permit.release()
	}
	prepare := followerPrepare(t, n, 65)
	if err := n.Handle(prepare); err == nil {
		t.Fatal("probe admitted beyond concurrency limit")
	}
	if n.remotePrepare != nil || n.probe != nil || len(n.seenPrepares) != 0 {
		t.Fatal("failed admission consumed authenticated prepare")
	}
	permits[0].release()
	discovery := n.discoveryBudget
	if err := n.Handle(prepare); err != nil {
		t.Fatal(err)
	}
	if n.probe == nil || n.remotePrepare == nil || n.discoveryBudget != discovery {
		t.Fatal("retry did not create probe within original discovery window")
	}
	probe := n.probe
	if err := n.Handle(prepare); err != nil {
		t.Fatal(err)
	}
	if n.probe != probe {
		t.Fatal("accepted prepare replay renewed probe")
	}
}

func TestNegotiationInitiatorRequiresStableDirectAndExactCommittedACK(t *testing.T) {
	n, f := negotiationFixture(t, false)
	if !n.initiator {
		t.Fatal("smaller node did not initiate")
	}
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	if n.prepare == nil || n.probe != nil {
		t.Fatal("Direct started before prepared acknowledgement")
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("prepare selected Direct")
	}
	for trial := 0; trial < 3; trial++ {
		if trial > 0 {
			f.clock.advance(stableProbeSpacing, stableProbeSpacing)
			if err := n.Tick(); err != nil {
				t.Fatal(err)
			}
		}
		prepared := replyControl(*n.prepare, transport.Prepared)
		if err := n.Handle(prepared); err != nil {
			t.Fatal(err)
		}
		original := n.probe
		if err := n.Handle(prepared); err != nil || n.probe != original {
			t.Fatal("ACK replay renewed probe", err)
		}
		completeNegotiatedProbe(t, n)
		if trial < 2 && n.commit != nil {
			t.Fatal("unstable Direct sent commit")
		}
	}
	if n.commit == nil {
		t.Fatal("stable Direct did not request commit")
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("lost ACK selected initiator Direct")
	}
	bad := replyControl(*n.commit, transport.Committed)
	bad.Nonce = bytes.Repeat([]byte{4}, 32)
	if err := n.Handle(bad); err == nil {
		t.Fatal("foreign ACK selected Direct")
	}
	ack := replyControl(*n.commit, transport.Committed)
	if err := n.Handle(ack); err != nil {
		t.Fatal(err)
	}
	activation, err := n.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	if err := activation.check(); err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(activation), fmt.Sprintf("%#v", activation)} {
		if rendered != "[bilaterally confirmed Direct activation]" {
			t.Fatal("unsafe activation formatting")
		}
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("activation emitted twice")
	}
	if err := n.Handle(ack); err != nil {
		t.Fatal("idempotent committed ACK rejected", err)
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("ACK replay emitted second activation")
	}
	f.lease.Revoke()
	if err := activation.check(); err == nil {
		t.Fatal("revocation accepted late activation")
	}
}

func TestNegotiationFollowerWaitsForOwnQualificationAndCachesACKWithoutRenewal(t *testing.T) {
	n, f := negotiationFixture(t, true)
	if n.initiator {
		t.Fatal("larger node initiated")
	}
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	if n.prepare != nil || n.probe != nil {
		t.Fatal("follower initiated concurrent candidate")
	}
	first := followerPrepare(t, n, 11)
	if err := n.Handle(first); err != nil {
		t.Fatal(err)
	}
	probe := n.probe
	if err := n.Handle(first); err != nil || n.probe != probe {
		t.Fatal("prepare replay renewed follower probe", err)
	}
	for trial := 0; trial < 3; trial++ {
		if trial > 0 {
			f.clock.advance(stableProbeSpacing, stableProbeSpacing)
			next := followerPrepare(t, n, byte(11+trial))
			if err := n.Handle(next); err != nil {
				t.Fatal(err)
			}
		}
		completeNegotiatedProbe(t, n)
	}
	if n.qualified == nil {
		t.Fatal("follower did not qualify own Direct path")
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("follower selected without commit")
	}
	commit := *n.remotePrepare
	commit.Kind = transport.Commit
	commit.Nonce = bytes.Repeat([]byte{21}, 32)
	if err := n.Handle(commit); err != nil {
		t.Fatal(err)
	}
	activation, err := n.TakeActivation()
	if err != nil {
		t.Fatal(err)
	}
	if err := activation.check(); err != nil {
		t.Fatal(err)
	}
	acknowledged := n.acknowledgement
	originalCycle := n.cycle
	if err := n.Handle(commit); err != nil || n.acknowledgement != acknowledged || n.cycle != originalCycle {
		t.Fatal("ACK retry changed original transcript/cycle", err)
	}
	if err := n.Handle(first); err == nil {
		t.Fatal("old prepare replaced newer follower trial")
	}
	f.clock.advance(30*time.Second, 30*time.Second)
	if err := n.Handle(commit); err == nil {
		t.Fatal("cached ACK renewed expired cycle")
	}
}

func TestNegotiationCommitBeforeLocalProofDoesNotSelectAndChangedScopeIsDenied(t *testing.T) {
	n, f := negotiationFixture(t, true)
	prepare := followerPrepare(t, n, 31)
	if err := n.Handle(prepare); err != nil {
		t.Fatal(err)
	}
	commit := prepare
	commit.Kind = transport.Commit
	commit.Nonce = bytes.Repeat([]byte{32}, 32)
	if err := n.Handle(commit); err != nil {
		t.Fatal(err)
	}
	if n.pendingCommit == nil || n.acknowledgement != nil {
		t.Fatal("early commit selected unqualified Direct")
	}
	if _, err := n.TakeActivation(); err == nil {
		t.Fatal("peer assertion replaced local Direct evidence")
	}
	originalCycle := n.cycle
	originalSocket := n.direct.bind.socket
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	f.refresh(t, &cursor)
	if err := n.relay.bind.mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(commit); err == nil {
		t.Fatal("old revision commit accepted")
	}
	if n.cycle != originalCycle || n.direct.bind.socket != originalSocket || n.pendingCommit != nil {
		t.Fatal("map refresh renewed cycle, churned listener, or retained commit")
	}
	if n.probe != nil || n.history.latest != nil {
		t.Fatal("new revision inherited old proof window")
	}
}

func TestNegotiationUnrelatedRevisionFasterThanRTTPreservesOriginalProbe(t *testing.T) {
	n, f := negotiationFixture(t, false)
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	originalCycle, history := n.cycle, n.history
	for trial := 0; trial < 3; trial++ {
		if trial > 0 {
			f.clock.advance(stableProbeSpacing, stableProbeSpacing)
			if err := n.Tick(); err != nil {
				t.Fatal(err)
			}
		}
		request := *n.prepare
		for update := 0; update < 3; update++ {
			cursor := f.verified.Cursor()
			f.clock.advance(20*time.Millisecond, 20*time.Millisecond)
			f.snapshot.Revision.Network++
			f.snapshot.Network.Revision = f.snapshot.Revision.Network
			f.refresh(t, &cursor)
			if err := n.relay.bind.mux.Update(f.verified); err != nil {
				t.Fatal(err)
			}
			if err := n.direct.Update(t.Context(), f.verified); err != nil {
				t.Fatal(err)
			}
			if err := n.Tick(); err != nil {
				t.Fatal(err)
			}
		}
		if !sameControl(request, *n.prepare) || n.history != history {
			t.Fatal("unrelated revisions renewed the frozen request or lost stability")
		}
		if err := n.Handle(replyControl(request, transport.Prepared)); err != nil {
			t.Fatal(err)
		}
		completeNegotiatedProbe(t, n)
		if trial < 2 && n.commit != nil {
			t.Fatal("revision churn bypassed the five-second stability span")
		}
	}
	if n.commit == nil || n.cycle != originalCycle || len(n.history.seen) != 3 {
		t.Fatal("global revision churn prevented unchanged Direct stability")
	}
	if err := n.Handle(replyControl(*n.commit, transport.Committed)); err != nil {
		t.Fatal(err)
	}
	activation, err := n.TakeActivation()
	if err != nil || activation.check() != nil {
		t.Fatal("fresh proof under current pair authority did not authorize exact bilateral activation", err)
	}
	if remaining, err := n.cycle.timer.Remaining(); err != nil || remaining > 25*time.Second {
		t.Fatal("revision updates renewed the original search budget", remaining, err)
	}
}

func TestNegotiationChangedPairGrantResetsCompletedStability(t *testing.T) {
	n, f := negotiationFixture(t, false)
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(replyControl(*n.prepare, transport.Prepared)); err != nil {
		t.Fatal(err)
	}
	completeNegotiatedProbe(t, n)
	history := n.history
	cursor := f.verified.Cursor()
	f.clock.advance(2*time.Second, 2*time.Second)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	f.refresh(t, &cursor)
	if err := n.relay.bind.mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	if n.history == history || len(n.history.seen) != 0 || n.history.latest != nil {
		t.Fatal("changed directed grant inherited stability")
	}
}

func TestNegotiationRestoredGrantDoesNotRestoreOldNonceOrTranscript(t *testing.T) {
	n, f := negotiationFixture(t, false)
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	prepared := replyControl(*n.prepare, transport.Prepared)
	if err := n.Handle(prepared); err != nil {
		t.Fatal(err)
	}
	oldProbe, oldCycle := n.probe, n.cycle
	port := f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port
	for _, nextPort := range []int{8443, port} {
		cursor := f.verified.Cursor()
		f.clock.advance(20*time.Millisecond, 20*time.Millisecond)
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision = f.snapshot.Revision.Network
		f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = nextPort
		f.refresh(t, &cursor)
		if err := n.relay.bind.mux.Update(f.verified); err != nil {
			t.Fatal(err)
		}
		if err := n.direct.Update(t.Context(), f.verified); err != nil {
			t.Fatal(err)
		}
		if err := n.Tick(); err != nil {
			t.Fatal(err)
		}
	}
	if n.Handle(prepared) == nil || n.direct.bind.revisionAllowed(prepared.NetworkRevision) || n.direct.bind.revisionAllowed(f.snapshot.Revision.Network+1) {
		t.Fatal("restoring grants restored an old transcript or admitted a future revision")
	}
	if oldProbe.receive(replyControl(oldProbe.request, transport.Echo)) == nil || n.cycle != oldCycle {
		t.Fatal("restoring grants revived an old nonce or reset the parent cycle")
	}
	if err := n.Handle(replyControl(*n.prepare, transport.Prepared)); err != nil {
		t.Fatal("current semantic scope could not begin a fresh probe", err)
	}
	completeNegotiatedProbe(t, n)
	if len(n.history.seen) != 1 || n.commit != nil {
		t.Fatal("restored scope inherited qualification")
	}
}

func TestNegotiationSilentProfileCannotRenewDiscoveryWithGlobalRevisions(t *testing.T) {
	n, f := negotiationFixture(t, false)
	original := n.discoveryBudget
	for update := 0; update < 3; update++ {
		if err := n.Tick(); err != nil {
			t.Fatal(err)
		}
		cursor := f.verified.Cursor()
		f.clock.advance(time.Second, time.Second)
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision = f.snapshot.Revision.Network
		f.refresh(t, &cursor)
		if err := n.relay.bind.mux.Update(f.verified); err != nil {
			t.Fatal(err)
		}
		if err := n.direct.Update(t.Context(), f.verified); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Tick(); err == nil || n.discoveryBudget != original {
		t.Fatal("revision churn renewed a silent candidate's discovery phase")
	}
}

func TestNegotiationGrantChangeWithoutRevisionStillInvalidatesProof(t *testing.T) {
	n, f := negotiationFixture(t, false)
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	prepared := replyControl(*n.prepare, transport.Prepared)
	if err := n.Handle(prepared); err != nil {
		t.Fatal(err)
	}
	completeNegotiatedProbe(t, n)
	history, proof := n.history, n.probe
	cursor := f.verified.Cursor()
	f.clock.advance(time.Second, time.Second)
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	f.refresh(t, &cursor)
	if err := n.relay.bind.mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	if err := n.direct.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	if _, err := proof.Evidence(); err == nil {
		t.Fatal("changed grant retained old own proof")
	}
	if err := n.Handle(prepared); err == nil {
		t.Fatal("old transcript survived a semantic change")
	}
	if err := n.Tick(); err != nil {
		t.Fatal(err)
	}
	if n.history == history || len(n.history.seen) != 0 {
		t.Fatal("same-revision semantic change retained stability")
	}
}

func TestNegotiationRepeatedPrepareDoesNotExtendThreeSecondProbe(t *testing.T) {
	n, f := negotiationFixture(t, true)
	prepare := followerPrepare(t, n, 41)
	if err := n.Handle(prepare); err != nil {
		t.Fatal(err)
	}
	original := n.probe
	f.clock.advance(3*time.Second, 3*time.Second)
	if err := n.Handle(prepare); err == nil || n.probe != original {
		t.Fatal("repeat prepare renewed expired probe")
	}
}

func TestNegotiationFreezesIncomingPrepareNonce(t *testing.T) {
	n, _ := negotiationFixture(t, true)
	prepare := followerPrepare(t, n, 51)
	original := prepare
	original.Nonce = bytes.Clone(prepare.Nonce)
	if err := n.Handle(prepare); err != nil {
		t.Fatal(err)
	}
	probe := n.probe
	prepare.Nonce[0] ^= 0xff
	if !sameControl(*n.remotePrepare, original) {
		t.Fatal("caller mutated frozen prepare transcript")
	}
	if err := n.Handle(original); err != nil || n.probe != probe {
		t.Fatal("original replay no longer idempotent", err)
	}
}
