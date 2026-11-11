package dataplane

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func pathChallengeFixture(t *testing.T) (*assignedCipher, *candidateFixture, *candidateSocketFixture, *pathChallenge, *atomic.Int32) {
	t.Helper()
	f := newCandidateFixture(t)
	socket := newCandidateSocketFixture()
	delivered := &atomic.Int32{}
	channel, err := buildAssignedCipher(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 576, f.clock, func([]byte) error { delivered.Add(1); return nil }, func(uint16) (candidateSocket, error) { return socket, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	const candidateID = "112233445566778899aabbccddeeff00"
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	proof, err := channel.BeginProbe(cycle, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	return channel, f, socket, proof, delivered
}

// Injecting post-crypto plaintext exercises binding/replay/clock state only;
// actual pinned crypto roundtrip is checked separately over memory datagrams.
func TestPathChallengeOwnNonceOneUseAndCurrentBinding(t *testing.T) {
	channel, f, _, probe, delivered := pathChallengeFixture(t)
	for _, rendered := range []string{fmt.Sprint(probe), fmt.Sprintf("%#v", probe)} {
		if rendered != "[private Direct challenge]" {
			t.Fatal("unsafe proof formatting")
		}
	}
	echo := replyControl(probe.request, transport.Echo)
	if err := probe.receive(echo); err == nil {
		t.Fatal("unsent nonce confirmed Direct")
	}
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*transport.Control)
	}{
		{"nonce", func(c *transport.Control) { c.Nonce = bytes.Repeat([]byte{8}, 32) }},
		{"pair", func(c *transport.Control) { c.SourceNodeID = "another-peer" }},
		{"generation", func(c *transport.Control) { c.SourceGeneration++ }},
		{"revision", func(c *transport.Control) { c.NetworkRevision++ }},
		{"profile", func(c *transport.Control) { c.ProfileDigest = fmt.Sprintf("%064x", 1) }},
		{"candidate", func(c *transport.Control) { c.CandidateID = "2233445566778899aabbccddeeff0011" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			value := echo
			change.mutate(&value)
			if err := probe.receive(value); err == nil {
				t.Fatal("foreign binding confirmed Direct")
			}
		})
	}
	f.clock.advance(time.Millisecond, time.Millisecond)
	local, peer, err := probe.assignment.addresses()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := controlPacket(peer, local, echo)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.receivePlaintext(packet); err != nil {
		t.Fatal(err)
	}
	first, err := probe.Evidence()
	if err != nil || first.RTT != time.Millisecond {
		t.Fatal("current own-nonce echo did not confirm", first, err)
	}
	f.clock.advance(time.Millisecond, time.Millisecond)
	if err := channel.receivePlaintext(packet); err != nil {
		t.Fatal(err)
	}
	repeated, err := probe.Evidence()
	if err != nil || repeated != first {
		t.Fatal("duplicate echo changed proof receipt", err)
	}
	packet[len(packet)-1] ^= 1
	if err := channel.receivePlaintext(packet); err != nil {
		t.Fatal(err)
	}
	if delivered.Load() != 0 {
		t.Fatal("control or malformed control escaped to application")
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	if _, err := probe.Evidence(); err == nil {
		t.Fatal("late proof remained eligible")
	}
}

func TestPathChallengeDoesNotTreatPeerChallengeAsOwnProof(t *testing.T) {
	channel, _, _, probe, delivered := pathChallengeFixture(t)
	incoming := replyControl(probe.request, transport.Echo)
	incoming.Kind = transport.Challenge
	incoming.Nonce = bytes.Repeat([]byte{6}, 32)
	if err := probe.receive(incoming); err != nil {
		t.Fatal("valid peer challenge could not be echoed", err)
	}
	if _, err := probe.Evidence(); err == nil {
		t.Fatal("peer-selected nonce confirmed our Direct path")
	}
	next, err := channel.BeginProbe(probe.attempt.cycle, probe.request.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(next.request.Nonce, probe.request.Nonce) {
		t.Fatal("own nonce reused across attempts")
	}
	if err := probe.receive(replyControl(probe.request, transport.Echo)); err == nil {
		t.Fatal("old attempt accepted late echo")
	}
	select {
	case <-probe.attempt.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("superseded attempt was not cancelled")
	}
	if delivered.Load() != 0 {
		t.Fatal("peer control reached application")
	}
}

func TestPathChallengeUnrelatedRevisionDoesNotCancelInFlightRoundtrip(t *testing.T) {
	channel, f, _, probe, _ := pathChallengeFixture(t)
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	original := probe.request
	for update := 0; update < 3; update++ {
		cursor := f.verified.Cursor()
		f.clock.advance(20*time.Millisecond, 20*time.Millisecond)
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision = f.snapshot.Revision.Network
		f.refresh(t, &cursor)
		if err := channel.Update(t.Context(), f.verified); err != nil {
			t.Fatal(err)
		}
	}
	wrong := replyControl(original, transport.Echo)
	wrong.NetworkRevision = f.snapshot.Revision.Network
	if err := probe.receive(wrong); err == nil {
		t.Fatal("reply rewrote the request revision")
	}
	challenge := replyControl(original, transport.Challenge)
	challenge.NetworkRevision = f.snapshot.Revision.Network + 1
	if err := probe.receive(challenge); err == nil {
		t.Fatal("future revision challenge accepted")
	}
	challenge.NetworkRevision--
	challenge.Nonce = bytes.Repeat([]byte{66}, 32)
	if err := probe.receive(challenge); err != nil {
		t.Fatal("current peer request could not be echoed under unchanged authority", err)
	}
	if err := probe.receive(replyControl(original, transport.Echo)); err != nil {
		t.Fatal("original in-flight echo lost to unrelated revision churn", err)
	}
	if evidence, err := probe.Evidence(); err != nil || evidence.RTT != 60*time.Millisecond {
		t.Fatal("roundtrip lost its original send time", evidence, err)
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	if _, err := probe.Evidence(); err == nil {
		t.Fatal("revision churn renewed the original three-second deadline")
	}
}

func TestPathChallengeChangedAuthorityCannotBeReopenedByEcho(t *testing.T) {
	for _, action := range []string{"grant", "revoke", "cancel"} {
		t.Run(action, func(t *testing.T) {
			channel, f, _, probe, _ := pathChallengeFixture(t)
			if err := probe.Send(); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "grant":
				cursor := f.verified.Cursor()
				f.clock.advance(time.Millisecond, time.Millisecond)
				f.snapshot.Revision.Network++
				f.snapshot.Network.Revision = f.snapshot.Revision.Network
				f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
				f.refresh(t, &cursor)
				if err := channel.Update(t.Context(), f.verified); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				f.lease.Revoke()
			case "cancel":
				probe.attempt.cancel()
			}
			if err := probe.receive(replyControl(probe.request, transport.Echo)); err == nil {
				t.Fatal("late echo restored invalid authority")
			}
			if _, err := probe.Evidence(); err == nil {
				t.Fatal("invalidated proof still selected")
			}
			select {
			case <-probe.done:
				t.Fatal("invalid echo emitted success")
			default:
			}
		})
	}
	if _, err := newProbeCycle(context.Background(), nil, "profile", 1, "candidate"); err == nil {
		t.Fatal("missing counter clock accepted")
	}
}
