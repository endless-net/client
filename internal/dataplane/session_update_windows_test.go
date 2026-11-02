//go:build windows

package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
	"github.com/tailscale/wireguard-go/device"
)

func nextSessionMap(t *testing.T, s *WindowsSession, previous connectivity.Map, clock *sessionClock, change func(*coordinator.NetworkMapSnapshot)) connectivity.Map {
	t.Helper()
	value, err := s.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := value.Profiles[s.profileID]
	snapshot, err := previous.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Revision.Network++
	snapshot.Network.Revision = snapshot.Revision.Network
	change(&snapshot)
	sample, err := clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	expires := sample.Wall.Add(time.Minute)
	for i := range snapshot.Peers {
		snapshot.Peers[i].EndpointExpiresAt = &expires
	}
	trust, err := control.DecodeAnnouncement(p.ControlOrigin, p.Trust.Bundle, sample.Wall)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{61}, 32))
	payload, err := coordinator.NetworkMapSigningPayload(snapshot, trust.KeyID, sample.Wall, expires)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	snapshot.MapSignature = &coordinator.MapSignature{Version: 5, Algorithm: "ed25519-network-map-v5", KeyID: trust.KeyID, IssuedAt: sample.Wall, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))}
	event := coordinator.MapStreamEvent{Type: "resync", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + snapshot.MapSignature.PayloadHash, From: previous.Cursor().Revision, To: snapshot.Revision, Snapshot: &snapshot, ResultSignature: snapshot.MapSignature}
	if p.Connection.MapCursor == nil {
		event.Type = "snapshot"
		event.From = coordinator.MapRevision{}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	v, err := connectivity.NewVerifier(p, connectivity.VerifierConfig{Now: func() time.Time { return sample.Wall }, Clock: clock, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192, Announcements: sessionAnnouncements{trust}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(context.Background(), raw, p.Connection.MapCursor)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSessionRefreshPreservesPeerObjectPermitIDsAndNativeAddress(t *testing.T) {
	s, _, _, m, ip, dynamic, guards, tunnel, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := m.Snapshot()
	key := peerKey(t, snapshot.Peers[0].PublicKey)
	d := s.engine.device.(*device.Device)
	peer := d.LookupPeer(key)
	ids := make(map[wf.RuleID]uint64)
	for key, value := range dynamic.live {
		ids[key] = value
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		snapshot.Peers[0].Endpoint = "192.0.2.42:51822"
		snapshot.Peers[0].EndpointGeneration++
	})
	if err := s.Refresh(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if d.LookupPeer(key) != peer || !reflect.DeepEqual(ids, dynamic.live) || ip.createdAddresses != 1 || ip.createdRoutes != 1 || tunnel.closes != 0 || len(guards.live) == 0 {
		t.Fatal("map refresh churned established peer/policy/native state")
	}
	if err := s.Refresh(context.Background(), next); !errors.Is(err, connectivity.ErrReplay) || !s.lease.Permissions().NewFlows {
		t.Fatal("replay mutated authority/lifetime", err)
	}
}

func TestSessionOfflineReauthorizationKeepsOriginalDeadlineAndPeer(t *testing.T) {
	s, _, _, m, _, _, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	expires := s.lease.Permissions().ExpiresAt
	snapshot, _ := m.Snapshot()
	key := peerKey(t, snapshot.Peers[0].PublicKey)
	d := s.engine.device.(*device.Device)
	peer := d.LookupPeer(key)
	clock.advance(20 * time.Second)
	if err := s.Offline(context.Background()); err != nil {
		t.Fatal(err)
	}
	permission := s.lease.Permissions()
	if permission.NewFlows || !permission.ExistingFlows || permission.ExpiresAt != expires || d.LookupPeer(key) != peer {
		t.Fatal("offline reset/widened authority or discarded live session")
	}
	fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: uint64(12345), wf.FieldIPLocalAddress: netip.MustParseAddr("100.70.0.1"), wf.FieldIPRemoteAddress: netip.MustParseAddr("100.70.0.2"), wf.FieldIPProtocol: wf.IPProto(6), wf.FieldIPLocalPort: uint16(50000), wf.FieldIPRemotePort: uint16(443), wf.FieldFlags: wf.ConditionFlag(0)}
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
		t.Fatal("offline compiled policy permits a new flow")
	}
	fields[wf.FieldFlags] = wf.ConditionFlagIsReauthorize
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit {
		t.Fatal("offline policy lost existing-flow reauthorization")
	}
	if err := s.Refresh(context.Background(), m); !errors.Is(err, connectivity.ErrReplay) || s.lease.Permissions().NewFlows {
		t.Fatal("replay restored online permission", err)
	}
	clock.advance(40 * time.Second)
	if err := s.Offline(context.Background()); !errors.Is(err, connectivity.ErrExpired) || s.lease.Permissions().ExistingFlows {
		t.Fatal("repeated offline extended original deadline", err)
	}
}

func TestSessionFreshSignedMapCanResumeOfflineWithoutRecreatingPeer(t *testing.T) {
	s, _, _, m, ip, _, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	if err := s.Offline(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(*coordinator.NetworkMapSnapshot) {})
	if err := s.Refresh(context.Background(), next); err != nil || !s.lease.Permissions().NewFlows || ip.createdAddresses != 1 {
		t.Fatal("fresh current map did not resume existing session", err)
	}
}

func addedPeerSnapshot(t *testing.T, snapshot *coordinator.NetworkMapSnapshot) {
	t.Helper()
	key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{65}, 32))
	if err != nil {
		t.Fatal(err)
	}
	peer := snapshot.Peers[0]
	peer.ID, peer.PublicKey, peer.Endpoint = "second-peer", base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), "192.0.2.2:51820"
	peer.AllowedIPs = []string{"100.70.0.3/32"}
	peer.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.3/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}
	snapshot.Peers = append(snapshot.Peers, peer)
}

func TestSessionRefreshAddsPeerWithGuardsBeforeNewRoute(t *testing.T) {
	s, store, _, m, ip, _, guards, tunnel, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { addedPeerSnapshot(t, snapshot) })
	if err := s.Refresh(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Profiles[s.profileID].Connection.Journal.Routes) != 2 || ip.createdRoutes != 2 || ip.createdAddresses != 1 || tunnel.closes != 0 || len(guards.live) != 6 {
		t.Fatal("added peer widened path without journal/guard or recreated adapter")
	}
}

func TestSessionRefreshLostACKNeverResumesOldGrant(t *testing.T) {
	for failedAt := 1; failedAt <= 8; failedAt++ {
		s, store, slot, m, _, dynamic, _, _, clock := wiredSessionFixture(t)
		if err := startSessionFixture(t, s, m); err != nil {
			t.Fatal(err)
		}
		clock.advance(time.Second)
		next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { addedPeerSnapshot(t, snapshot) })
		slot.loseACKAt = slot.writes + failedAt
		if err := s.Refresh(context.Background(), next); !errors.Is(err, state.ErrUncertain) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
			t.Fatal("lost refresh ACK returned success/access", failedAt, err)
		}
		slot.loseACKAt = 0
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal("lost refresh ACK left unresolvable cleanup", failedAt, err)
		}
		value, err := store.Load(context.Background())
		if err != nil || value.Profiles[s.profileID].Connection.Journal != nil {
			t.Fatal("refresh cleanup not retired", failedAt, err)
		}
	}
}

func TestSessionRefreshAndOfflineNativeUncertaintyRemainFailClosed(t *testing.T) {
	for _, failure := range []string{"refresh-commit", "route-create", "offline-commit", "route-replaced"} {
		s, store, _, m, ip, dynamic, _, _, clock := wiredSessionFixture(t)
		if err := startSessionFixture(t, s, m); err != nil {
			t.Fatal(err)
		}
		clock.advance(time.Second)
		next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { addedPeerSnapshot(t, snapshot) })
		switch failure {
		case "refresh-commit", "offline-commit":
			dynamic.fail = "commit"
		case "route-create":
			ip.loseCreate = true
		case "route-replaced":
			ip.forward[len(ip.forward)-1].Metric++
		}
		var err error
		if failure == "offline-commit" {
			err = s.Offline(context.Background())
		} else {
			err = s.Refresh(context.Background(), next)
		}
		if err == nil || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
			t.Fatal("native update uncertainty left traffic authority", failure, err)
		}
		dynamic.fail = ""
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal("failed update could not reconcile", failure, err)
		}
		value, err := store.Load(context.Background())
		if err != nil || value.Profiles[s.profileID].Connection.Journal != nil {
			t.Fatal("failed native update lost cleanup", failure, err)
		}
	}
}

func TestSessionVerifiedRevocationStopsBothFlowClassesAndCannotRefreshBack(t *testing.T) {
	s, _, _, m, _, dynamic, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	revoked := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Node.Status = "revoked"; snapshot.Peers = nil })
	if !revoked.Denied() {
		t.Fatal("fixture did not verify revocation")
	}
	if err := s.Refresh(context.Background(), revoked); !errors.Is(err, connectivity.ErrRevoked) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
		t.Fatal("revocation retained flow authority", err)
	}
	if err := s.Refresh(context.Background(), m); err != ErrClosed {
		t.Fatal("revoked session restored old map", err)
	}
}

func TestSessionOfflineLostACKNeverLeavesEitherFlowClassEnabled(t *testing.T) {
	for failedAt := 1; failedAt <= 3; failedAt++ {
		s, _, slot, m, _, dynamic, _, _, _ := wiredSessionFixture(t)
		if err := startSessionFixture(t, s, m); err != nil {
			t.Fatal(err)
		}
		slot.loseACKAt = slot.writes + failedAt
		if err := s.Offline(context.Background()); !errors.Is(err, state.ErrUncertain) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
			t.Fatal("offline metadata uncertainty retained traffic", failedAt, err)
		}
		slot.loseACKAt = 0
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionStopPreemptsInFlightRefreshWithoutLateResume(t *testing.T) {
	s, _, _, m, _, dynamic, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(*coordinator.NetworkMapSnapshot) {})
	entered := make(chan struct{})
	s.planFilters = func(ctx context.Context, _ connectivity.Map, _ string, _ uint64, _ uint32, _, _ int, _ bool) (FilterPlan, error) {
		close(entered)
		<-ctx.Done()
		return FilterPlan{}, ctx.Err()
	}
	refreshed := make(chan error, 1)
	go func() { refreshed <- s.Refresh(context.Background(), next) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh not reached")
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := <-refreshed; !errors.Is(err, context.Canceled) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
		t.Fatal("in-flight refresh resumed after Stop", err)
	}
}

func TestSessionPeerRemovalKeepsDenyAfterRouteDeletion(t *testing.T) {
	s, store, _, m, ip, _, guards, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := m.Snapshot()
	key := peerKey(t, snapshot.Peers[0].PublicKey)
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Peers = nil })
	if err := s.Refresh(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Profiles[cleanupProfile].Connection.Journal.Routes) != 0 || ip.deletedRoutes != 1 || s.engine.device.(*device.Device).LookupPeer(key) != nil || len(guards.live) != 4 {
		t.Fatal("removed peer lost its guard or retained route/session")
	}
}
