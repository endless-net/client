package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestProfileReloadPreservesSearchDeadlineAndWireQuota(t *testing.T) {
	f := newCandidateFixture(t)
	g := &transportSupervisor{snapshot: f.snapshot}
	p := &supervisedRelay{scope: packetBinding{peerID: "peer"}}
	g.refreshProfileSet(p)
	original := append([]coordinator.TransportProfileOffer{}, g.offers(p)...)
	id := original[0].Profile.ID
	parent, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, "reload")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.cancel()
	wire := &probeWireBudget{used: 65000}
	s := &directSearch{parent: parent, order: []string{id}, tried: map[string]bool{id: true}, seen: map[string]bool{"old-candidate": true}, traffic: map[string]*probeWireBudget{id: wire}}
	p.search = s
	f.clock.advance(29*time.Second, 29*time.Second)
	changed := original[0]
	changed.Digest = "changed-digest"
	g.snapshot.Peers[0].Transport.Profiles = []coordinator.TransportProfileOffer{changed}
	g.refreshProfileSet(p)
	if s.tried[id] || s.parent != parent || s.traffic[id] != wire || wire.consumed() != 65000 || !s.seen["old-candidate"] {
		t.Fatal("reload renewed search authority or quota, or skipped changed profile")
	}
	// Removing and restoring A must retain its original ledger and nonce fence.
	g.snapshot.Peers[0].Transport.Profiles = []coordinator.TransportProfileOffer{}
	g.refreshProfileSet(p)
	if len(s.order) != 0 {
		t.Fatal("removed profile retained in search order")
	}
	g.snapshot.Peers[0].Transport.Profiles = original
	g.refreshProfileSet(p)
	if len(s.order) != 1 || s.order[0] != id || s.traffic[id] != wire || !s.seen["old-candidate"] {
		t.Fatal("returning profile lost original accounting")
	}
	f.clock.advance(time.Second, time.Second)
	if err := parent.check(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reload extended original deadline: %v", err)
	}
}

func TestProfileReloadResetsCooldownOnlyForChangedProfileSet(t *testing.T) {
	f := newCandidateFixture(t)
	g := &transportSupervisor{snapshot: f.snapshot}
	p := &supervisedRelay{scope: packetBinding{peerID: "peer"}}
	g.refreshProfileSet(p)
	sample, err := f.clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	delay, err := connectivity.NewRetryDelay(f.clock, f.profile.ID, 1, sample, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p.cooldown, p.failures, p.resumeProfile = delay, 6, "resume"
	g.snapshot.Revision.Network++
	g.snapshot.Peers[0].Transport.Profiles[0].PeerEndpoint = "192.0.2.99:51820"
	g.refreshProfileSet(p)
	if p.cooldown != delay || p.failures != 6 || p.resumeProfile != "resume" {
		t.Fatal("unrelated map renewal or endpoint ACK reset backoff")
	}
	g.snapshot.Peers[0].Transport.Profiles[0].Digest = "changed-digest"
	g.refreshProfileSet(p)
	if p.cooldown != nil || p.failures != 0 || p.resumeProfile != "" {
		t.Fatal("new profile configuration did not resume discovery")
	}
}
