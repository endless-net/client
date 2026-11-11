package dataplane

import (
	"fmt"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
)

func recordDirectProof(t *testing.T, history *directStability, probe *pathChallenge) {
	t.Helper()
	if err := probe.Send(); err != nil {
		t.Fatal(err)
	}
	if err := probe.receive(replyControl(probe.request, transport.Echo)); err != nil {
		t.Fatal(err)
	}
	if err := history.Record(probe); err != nil {
		t.Fatal(err)
	}
}

func TestDirectStabilityNeedsThreeIndependentProofsSpanningFiveSeconds(t *testing.T) {
	channel, f, _, first, _ := pathChallengeFixture(t)
	history, err := newDirectStability(channel, first.attempt.cycle)
	if err != nil {
		t.Fatal(err)
	}
	recordDirectProof(t, history, first)
	for range 5 {
		if err := history.Record(first); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := history.Qualify(); err == nil {
		t.Fatal("duplicate echo counted as independent probes")
	}
	f.clock.advance(2500*time.Millisecond, 2500*time.Millisecond)
	second, err := channel.BeginProbe(first.attempt.cycle, first.request.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	recordDirectProof(t, history, second)
	if _, err := history.Qualify(); err == nil {
		t.Fatal("two probes selected Direct")
	}
	f.clock.advance(2499*time.Millisecond, 2499*time.Millisecond)
	third, err := channel.BeginProbe(first.attempt.cycle, first.request.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	recordDirectProof(t, history, third)
	if _, err := history.Qualify(); err == nil {
		t.Fatal("three probes before five seconds selected Direct")
	}
	f.clock.advance(time.Millisecond, time.Millisecond)
	fourth, err := channel.BeginProbe(first.attempt.cycle, first.request.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	recordDirectProof(t, history, fourth)
	qualified, err := history.Qualify()
	if err != nil {
		t.Fatal("stable candidate rejected", err)
	}
	if err := qualified.check(channel); err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(qualified), fmt.Sprintf("%#v", qualified)} {
		if rendered != "[qualified Direct candidate]" {
			t.Fatal("unsafe qualification formatting")
		}
	}
	if err := qualified.check(&assignedCipher{}); err == nil {
		t.Fatal("qualification transferred to another cipher")
	}
	f.clock.advance(3*time.Second, 3*time.Second)
	if err := qualified.check(channel); err == nil {
		t.Fatal("late activation reused expired last probe")
	}
}

func TestDirectStabilityRejectsOtherCycleAndGrantChange(t *testing.T) {
	channel, f, _, probe, _ := pathChallengeFixture(t)
	history, err := newDirectStability(channel, probe.attempt.cycle)
	if err != nil {
		t.Fatal(err)
	}
	recordDirectProof(t, history, probe)
	other, _, _, otherProbe, _ := pathChallengeFixture(t)
	if err := history.Record(otherProbe); err == nil {
		t.Fatal("other peer cipher proof counted")
	}
	_ = other
	cursor := f.verified.Cursor()
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	f.refresh(t, &cursor)
	if err := channel.Update(t.Context(), f.verified); err != nil {
		t.Fatal(err)
	}
	next, err := channel.BeginProbe(probe.attempt.cycle, probe.request.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Record(next); err == nil {
		t.Fatal("new revision inherited previous stability window")
	}
}
