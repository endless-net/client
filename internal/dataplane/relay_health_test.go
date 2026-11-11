package dataplane

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func relayHealthFixture(t *testing.T) (*relayCipher, *candidateFixture, *relayHealthProbe) {
	t.Helper()
	mux, f, _, _ := relayMuxFixture(t)
	channel, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "112233445566778899aabbccddeeff00")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	probe, err := channel.BeginHealth(cycle)
	if err != nil {
		t.Fatal(err)
	}
	return channel, f, probe
}

func TestRelayHealthOwnSentNonceAndOneUseReceipt(t *testing.T) {
	channel, f, probe := relayHealthFixture(t)
	for _, rendered := range []string{fmt.Sprint(probe), fmt.Sprintf("%#v", probe)} {
		if rendered != "[private Relay health challenge]" {
			t.Fatal("unsafe health formatting")
		}
	}
	echo := replyControl(probe.request, transport.Echo)
	if err := probe.receive(echo); err == nil {
		t.Fatal("unsent nonce confirmed Relay")
	}
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	wrong := echo
	wrong.Nonce = bytes.Repeat([]byte{8}, 32)
	if err := probe.receive(wrong); err == nil {
		t.Fatal("foreign nonce confirmed Relay")
	}
	f.clock.advance(time.Millisecond, time.Millisecond)
	if err := probe.receive(echo); err != nil {
		t.Fatal(err)
	}
	first, err := probe.Evidence()
	if err != nil || first.RTT != time.Millisecond {
		t.Fatal("valid health receipt rejected", first, err)
	}
	f.clock.advance(time.Millisecond, time.Millisecond)
	if err := probe.receive(echo); err != nil {
		t.Fatal(err)
	}
	second, err := probe.Evidence()
	if err != nil || second != first {
		t.Fatal("duplicate changed health receipt")
	}
	next, err := channel.BeginHealth(probe.attempt.cycle)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(next.request.Nonce, probe.request.Nonce) {
		t.Fatal("health nonce reused")
	}
	if err := probe.receive(echo); err == nil {
		t.Fatal("superseded Relay probe accepted reply")
	}
	if _, err := next.Evidence(); err == nil {
		t.Fatal("new health inherited previous proof")
	}
	channel.Close()
	select {
	case <-next.attempt.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("channel close retained health timer")
	}
}

func TestRelayHealthDoesNotTurnPeerChallengeIntoLocalProof(t *testing.T) {
	channel, _, probe := relayHealthFixture(t)
	incoming := replyControl(probe.request, transport.Echo)
	incoming.Kind = transport.Challenge
	incoming.Nonce = bytes.Repeat([]byte{7}, 32)
	channel.bind.mu.Lock()
	a := candidateAssignment{local: channel.bind.local, peer: channel.bind.peer}
	channel.bind.mu.Unlock()
	local, peer, err := a.addresses()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := controlPacket(peer, local, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.receivePlaintext(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Evidence(); err == nil {
		t.Fatal("peer-selected challenge confirmed our Relay path")
	}
	select {
	case <-channel.Events():
		t.Fatal("baseline health reached Direct negotiation events")
	default:
	}
}

func TestRelayHealthUnrelatedRevisionPreservesFrozenNonceAndDeadline(t *testing.T) {
	channel, f, probe := relayHealthFixture(t)
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
		if err := channel.bind.mux.Update(f.verified); err != nil {
			t.Fatal(err)
		}
	}
	if err := probe.receive(replyControl(original, transport.Echo)); err != nil {
		t.Fatal("in-flight Relay echo was canceled by unrelated revision", err)
	}
	if evidence, err := probe.Evidence(); err != nil || evidence.RTT != 60*time.Millisecond {
		t.Fatal("Relay proof lost original receipt", evidence, err)
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	if _, err := probe.Evidence(); err == nil {
		t.Fatal("revision churn renewed original Relay deadline")
	}
}

func TestRelayHealthExpiryAndRevocationFence(t *testing.T) {
	for _, action := range []string{"timeout", "grant", "revoke"} {
		t.Run(action, func(t *testing.T) {
			channel, f, probe := relayHealthFixture(t)
			if err := probe.Send(); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "timeout":
				f.clock.advance(3*time.Second, 3*time.Second)
			case "grant":
				cursor := f.verified.Cursor()
				f.clock.advance(time.Millisecond, time.Millisecond)
				f.snapshot.Revision.Network++
				f.snapshot.Network.Revision = f.snapshot.Revision.Network
				f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
				f.refresh(t, &cursor)
				if err := channel.bind.mux.Update(f.verified); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				f.lease.Revoke()
			}
			if err := probe.receive(replyControl(probe.request, transport.Echo)); err == nil {
				t.Fatal("late Relay health restored authority")
			}
			if _, err := probe.Evidence(); err == nil {
				t.Fatal("ineligible Relay probe observed")
			}
		})
	}
}

func TestExpiredDirectProfileDoesNotDisableAuthorizedRelayHealth(t *testing.T) {
	mux, f, _, _ := relayMuxFixture(t)
	f.planTTL = 5 * time.Second
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &cursor)
	if err := mux.Update(f.verified); err != nil {
		t.Fatal(err)
	}
	channel, err := newRelayCipher(t.Context(), mux, f.verified, f.lease, "peer", f.profile.Keys.WireGuardPrivate, 1280, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(channel.Close)
	direct := challengeControl(t, f)
	f.clock.advance(6*time.Second, 6*time.Second)
	if err := channel.QueueControl(direct); err == nil {
		t.Fatal("expired Direct profile admitted negotiation")
	}
	cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, direct.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cycle.cancel)
	health, err := channel.BeginHealth(cycle)
	if err != nil {
		t.Fatal("expired Direct plan disabled separate Relay authority", err)
	}
	if err := health.Send(); err != nil {
		t.Fatal("authorized Relay health stopped", err)
	}
}
