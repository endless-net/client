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

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	snapshot.MapSignature = &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: trust.KeyID, IssuedAt: sample.Wall, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))}
	event := coordinator.MapStreamEvent{Type: "resync", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + snapshot.MapSignature.PayloadHash, From: previous.Cursor().Revision, To: snapshot.Revision, Snapshot: &snapshot, ResultSignature: snapshot.MapSignature}
	if p.Connection.MapCursor == nil {
		event.Type = "snapshot"
		event.From = coordinator.MapRevision{}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	v, err := connectivity.NewVerifier(p, connectivity.VerifierConfig{IPv4ResourceRoutes: true, IPv4ExitRouting: true, UnavailableResourceGuards: true, IndependentGrantExpiry: true, ResolverDNS: true, Now: func() time.Time { return sample.Wall }, Clock: clock, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192, Announcements: sessionAnnouncements{trust}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(context.Background(), raw, p.Connection.MapCursor)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWindowsOfflineGrantExpiryUpdatesInstalledFilters(t *testing.T) {
	s, _, _, first, _, dynamic, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, first); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	sample, _ := clock.Sample()
	end := sample.Wall.Add(10 * time.Second)
	next := nextSessionMap(t, s, first, clock, func(m *coordinator.NetworkMapSnapshot) {
		peer := &m.Peers[0]
		peer.ACLGrants[0].ValidUntil = &end
		peer.ACLGrants = append(peer.ACLGrants, coordinator.ACLGrant{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}})
	})
	if err := s.Refresh(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	onlineEpoch := s.grantTimerEpoch
	if err := s.Offline(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.grantTimer == nil || !s.policy.nextGrantEnd().Equal(end) {
		t.Fatal("offline transition lost the signed deadline")
	}
	var timed, independent wf.RuleID
	for _, r := range s.policy.rules {
		if r.Layer == wf.LayerALEAuthConnectV4 && r.Action == wf.ActionPermit {
			if s.policy.grantEnds[r.ID].IsZero() {
				independent = r.ID
			} else {
				timed = r.ID
			}
		}
	}
	stableID := dynamic.live[independent]
	if stableID == 0 || dynamic.live[timed] == 0 {
		t.Fatal("missing installed permits before expiry")
	}
	clock.advance(10 * time.Second)
	if !s.lease.Permissions().ExistingFlows {
		t.Fatal("independent map lease expired with TCP/443")
	}
	s.onGrantTimer(onlineEpoch)
	if dynamic.live[timed] == 0 {
		t.Fatal("superseded online callback changed the offline plan")
	}
	s.onGrantTimer(s.grantTimerEpoch)
	if dynamic.live[timed] != 0 || dynamic.live[independent] != stableID || s.grantTimer != nil {
		t.Fatal("expiry retained a timed permit or replaced the independent filter")
	}
}

func TestWindowsGrantTimeRejectsRollbackWithinConfirmedMap(t *testing.T) {
	s, _, _, m, _, _, _, _, clock := wiredSessionFixture(t)
	clock.mu.Lock()
	clock.sample.Wall = clock.sample.Wall.Add(10 * time.Second)
	clock.mu.Unlock()
	if _, err := s.grantTime(m); err != nil {
		t.Fatal(err)
	}
	clock.mu.Lock()
	clock.sample.Wall = clock.sample.Wall.Add(-5 * time.Second)
	clock.mu.Unlock()
	if _, err := s.grantTime(m); !errors.Is(err, connectivity.ErrClock) {
		t.Fatal("clock rollback could revive expired WFP permits", err)
	}
}

// Commit an independent local change between every two storage transactions
// after the target cursor is recorded, as an endpoint worker can do.
type concurrentRefreshWriter struct {
	store  *state.Store
	slot   *cleanupSlot
	target state.MapCursor
	writes int
}

func (w *concurrentRefreshWriter) Exclusive(ctx context.Context, f func(state.Slot) error) error {
	value, err := w.store.Load(ctx)
	if err != nil {
		return err
	}
	p := value.Profiles[cleanupProfile]
	clearSessionKeys(value)
	if p.Connection.MapCursor != nil && *p.Connection.MapCursor == w.target {
		err = w.store.UpdateCurrent(ctx, func(v *state.State) error {
			v.HintObservedAt = v.HintObservedAt.Add(time.Second)
			return nil
		})
		if err != nil {
			return err
		}
		w.writes++
	}
	return w.slot.Exclusive(ctx, f)
}

func TestWindowsRefreshWithConcurrentStateWriter(t *testing.T) {
	s, store, slot, m, _, _, _, tunnel, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Peers[0].EndpointGeneration++ })
	v, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clearSessionKeys(v)
	w := &concurrentRefreshWriter{store: store, slot: slot, target: next.Cursor()}
	s.store, err = state.New(w, slot.codec, v.InstallationID, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Refresh(t.Context(), next); err != nil {
		t.Fatalf("refresh after %d competing commits: %v", w.writes, err)
	}
	v, err = store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearSessionKeys(v)
	ack := v.Profiles[cleanupProfile].Connection.LastAppliedMap
	if w.writes < 2 || ack == nil || *ack != next.Cursor() || tunnel.closes != 0 || !s.lease.Permissions().NewFlows {
		t.Fatal("new map was not applied on the existing tunnel")
	}
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

func TestEstablishedRouteRemainsUsableDuringEndpointRenewal(t *testing.T) {
	s, _, _, m, _, _, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		snapshot.Peers[0].EndpointGeneration++
	})
	snapshot, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	id := snapshot.Peers[0].ID
	s.packets.mu.Lock()
	binding, route, epoch := s.packets.pairs[id], s.packets.routes[id], s.packets.routeEpoch[id]
	s.packets.mu.Unlock()
	plan := s.planFilters
	s.planFilters = func(ctx context.Context, value connectivity.Map, owner string, luid uint64, index uint32, grants, protected int, offline bool) (FilterPlan, error) {
		if !s.packets.routeCurrent(binding, route, epoch) {
			return FilterPlan{}, errors.New("admitted packet lost its route during renewal")
		}
		return plan(ctx, value, owner, luid, index, grants, protected, offline)
	}
	if err := s.Refresh(WithSourceEpoch(t.Context(), s.MapEpoch(next)), next); err != nil {
		t.Fatal(err)
	}
	if !s.packets.routeCurrent(binding, route, epoch) {
		t.Fatal("route was invalidated by the new revision")
	}
}

func TestWindowsPeerChurnKeepsIndependentRouteDuringNativeApply(t *testing.T) {
	s, _, _, current, _, _, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, current); err != nil {
		t.Fatal(err)
	}
	snapshot, err := current.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	id := snapshot.Peers[0].ID
	dev := s.engine.device.(*device.Device)
	key := peerKey(t, snapshot.Peers[0].PublicKey)
	wgPeer := dev.LookupPeer(key)
	if wgPeer == nil {
		t.Fatal("initial WireGuard peer missing")
	}
	s.packets.mu.Lock()
	binding, route, epoch := s.packets.pairs[id], s.packets.routes[id], s.packets.routeEpoch[id]
	s.packets.mu.Unlock()
	live := func() bool { return s.packets.routeCurrent(binding, route, epoch) && dev.LookupPeer(key) == wgPeer }
	plan := s.planFilters
	s.planFilters = func(ctx context.Context, v connectivity.Map, owner string, luid uint64, index uint32, grants, protected int, offline bool) (FilterPlan, error) {
		if !live() {
			return FilterPlan{}, errors.New("independent route lost during native application")
		}
		return plan(ctx, v, owner, luid, index, grants, protected, offline)
	}
	for _, change := range []func(*coordinator.NetworkMapSnapshot){
		func(m *coordinator.NetworkMapSnapshot) { addedPeerSnapshot(t, m) },
		func(m *coordinator.NetworkMapSnapshot) {
			m.Peers[1].Endpoint = "192.0.2.99:51820"
			m.Peers[1].EndpointGeneration++
		},
		func(m *coordinator.NetworkMapSnapshot) { m.Peers = m.Peers[:1] },
	} {
		clock.advance(time.Second)
		next := nextSessionMap(t, s, current, clock, change)
		source := s.MapEpoch(next)
		if !live() {
			t.Fatal("map reception interrupted independent route")
		}
		if err := s.Refresh(WithSourceEpoch(t.Context(), source), next); err != nil {
			t.Fatal(err)
		}
		if !live() {
			t.Fatal("map application replaced independent session")
		}
		current = next
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

func TestOfflinePolicyReplacementCannotBeUndoneByOlderDelivery(t *testing.T) {
	s, store, _, first, _, filters, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, first); err != nil {
		t.Fatal(err)
	}
	var revoked wf.RuleID
	for _, r := range s.policy.rules {
		if r.Layer == wf.LayerALEAuthConnectV4 && r.Action == wf.ActionPermit {
			revoked = r.ID
		}
	}
	if revoked.IsZero() || filters.live[revoked] == 0 {
		t.Fatal("fixture needs an installed egress grant")
	}
	if err := s.Offline(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	replacement := nextSessionMap(t, s, first, clock, func(m *coordinator.NetworkMapSnapshot) {
		m.Peers[0].ACLGrants[0].AllowedPorts = []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}
	})
	if err := s.Refresh(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	if filters.live[revoked] != 0 || !s.lease.Permissions().NewFlows {
		t.Fatal("fresh online policy retained withdrawn grant")
	}
	fields := map[wf.FieldID]any{
		wf.FieldIPLocalInterface: uint64(12345), wf.FieldIPLocalAddress: netip.MustParseAddr("100.70.0.1"),
		wf.FieldIPRemoteAddress: netip.MustParseAddr("100.70.0.2"), wf.FieldIPProtocol: wf.IPProto(6),
		wf.FieldIPLocalPort: uint16(50000), wf.FieldIPRemotePort: uint16(22),
	}
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit {
		t.Fatal("replacement did not permit TCP/22")
	}
	fields[wf.FieldIPRemotePort] = uint16(443)
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
		t.Fatal("replacement allowed TCP/443")
	}
	if err := s.Refresh(t.Context(), first); err == nil || filters.live[revoked] != 0 {
		t.Fatal("stale delivery restored the original grant", err)
	}
	v, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := v.Profiles[s.profileID].Connection
	if c.MapCursor == nil || c.LastAppliedMap == nil || c.MapCursor.PayloadHash != replacement.Cursor().PayloadHash || c.LastAppliedMap.PayloadHash != replacement.Cursor().PayloadHash {
		t.Fatal("stale delivery replaced the target or application receipt")
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

func TestSessionRefreshWithdrawsManagedSubnet(t *testing.T) {
	s, store, _, first, ip, _, _, _, clock := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, first); err != nil {
		t.Fatal(err)
	}
	const target = "10.27.4.0/24"
	clock.advance(time.Second)
	now, _ := clock.Sample()
	active := nextSessionMap(t, s, first, clock, func(m *coordinator.NetworkMapSnapshot) {
		addedPeerSnapshot(t, m)
		app := coordinator.Application{ID: "printer", TargetType: "cidr", Target: target, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}}
		for i, p := range m.Peers {
			h := coordinator.ServiceHost{NodeID: p.ID, PublicKey: p.PublicKey}
			app.Connectors = append(app.Connectors, h)
			app.Routes = append(app.Routes, coordinator.ApplicationRoute{Connector: h, Metric: uint32(i + 1), CIDRs: []string{target}, ExpiresAt: now.Wall.Add(time.Minute)})
		}
		m.Network.Applications = []coordinator.Application{app}
	})
	if err := s.Refresh(t.Context(), active); err != nil {
		t.Fatal("add resource", err)
	}
	check := func(want bool) {
		t.Helper()
		value, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range value.Profiles[s.profileID].Connection.Journal.Routes {
			if r.Destination == target {
				if !want || !r.Resource || r.Phase != state.EffectApplied || r.Fingerprint == "" {
					t.Fatal("unexpected receipt", r)
				}
				return
			}
		}
		if want {
			t.Fatal("missing resource route")
		}
	}
	check(true)
	dst := netip.MustParseAddr("10.27.4.9")
	if peer := s.packets.resources.lookup(dst).peer; peer != "" {
		t.Fatal("resource opened before path confirmation", peer)
	}
	snapshot, err := active.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, ready := range [][]string{{"peer", "second-peer"}, {"second-peer"}, {}, {"peer"}} {
		clock.advance(4 * s.config.ProbeInterval)
		sample, _ := clock.Sample()
		statuses := make([]*clientv0.PeerTransportStatus, 0, len(ready))
		for _, id := range ready {
			statuses = append(statuses, &clientv0.PeerTransportStatus{PeerId: id, SelectedPath: clientv0.PathKind_PATH_KIND_DIRECT, CheckedAt: timestamppb.New(sample.Wall)})
		}
		if err := s.reconcileResourcePaths(t.Context(), statuses); err != nil {
			t.Fatal(err)
		}
		want := ""
		if len(ready) > 0 {
			want = ready[0]
		}
		if got := s.packets.resources.lookup(dst).peer; got != want {
			t.Fatal("packet switch", got, want)
		}
		for _, p := range snapshot.Peers {
			if (s.engine.bindings[p.PublicKey].resources == target) != (p.ID == want) {
				t.Fatal("WireGuard route", p.ID, want)
			}
		}
	}
	clock.advance(time.Second)
	withdrawn := nextSessionMap(t, s, active, clock, func(m *coordinator.NetworkMapSnapshot) { m.Network.Applications = nil })
	if err := s.Refresh(t.Context(), withdrawn); err != nil {
		t.Fatal("withdraw", err)
	}
	check(false)
	if ip.deletedRoutes != 1 {
		t.Fatal("wrong deletion count", ip.deletedRoutes)
	}
	clock.advance(time.Second)
	next := nextSessionMap(t, s, withdrawn, clock, func(*coordinator.NetworkMapSnapshot) {})
	if err := s.Refresh(t.Context(), next); err != nil {
		t.Fatal("following map", err)
	}
}

func TestWindowsSessionRefreshWithdrawsExitAndKeepsSubnet(t *testing.T) {
	s, store, _, first, _, _, guards, _, clock := wiredSessionFixture(t)
	s.config.ApplyTimeout = 20 * time.Second
	s.makeBind = func() conn.Bind { return new(outerBindFixture) }
	base := s.planFilters
	s.planFilters = func(ctx context.Context, m connectivity.Map, id string, luid uint64, index uint32, grants, protected int, offline bool) (FilterPlan, error) {
		p, err := base(ctx, m, id, luid, index, grants, protected, offline)
		if err != nil || len(p.exitGroups) == 0 {
			return p, err
		}
		return p.withExitTransport(`\device\harddiskvolume1\endlessnet\client.exe`)
	}
	if err := startSessionFixture(t, s, first); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second)
	sample, _ := clock.Sample()
	const subnet = "10.72.0.0/24"
	active := nextSessionMap(t, s, first, clock, func(m *coordinator.NetworkMapSnapshot) {
		peer := m.Peers[0]
		router := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
		for _, target := range []string{subnet, "0.0.0.0/0"} {
			id := "office"
			if target != subnet {
				id = "exit"
			}
			m.Network.Applications = append(m.Network.Applications, coordinator.Application{ID: id, TargetType: "cidr", Target: target, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Connectors: []coordinator.ServiceHost{router}, Routes: []coordinator.ApplicationRoute{{Connector: router, Metric: 1, CIDRs: []string{target}, ExpiresAt: sample.Wall.Add(time.Minute)}}})
		}
	})
	if err := s.Refresh(t.Context(), active); err != nil {
		t.Fatal("select exit", err)
	}
	value, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 65536 {
		t.Fatal("exit and subnet exceed installed plaintext state limit", len(encoded))
	}
	var exitIDs []wf.RuleID
	var office wf.RuleID
	for _, rule := range s.policy.rules {
		if prefix, ok := resourceGuardPrefix(rule); ok {
			if rule.Layer == wf.LayerALEAuthConnectV4 {
				exitIDs = append(exitIDs, rule.ID)
			}
			if prefix.String() == subnet {
				office = rule.ID
			}
		}
	}
	officeID := guards.live[office]
	if len(exitIDs) == 0 || officeID == 0 {
		t.Fatal("missing installed guards")
	}
	clock.advance(time.Second)
	sample, _ = clock.Sample()
	next := nextSessionMap(t, s, active, clock, func(m *coordinator.NetworkMapSnapshot) {
		m.Network.Applications = m.Network.Applications[:1]
		m.Network.Applications[0].Routes[0].ExpiresAt = sample.Wall.Add(time.Minute)
	})
	if err := s.Refresh(t.Context(), next); err != nil {
		t.Fatal("withdraw exit", err)
	}
	for _, id := range exitIDs {
		if guards.live[id] != 0 {
			t.Fatal("exit block still installed after refresh", id)
		}
	}
	if guards.live[office] != officeID {
		t.Fatal("independent subnet guard changed")
	}
	value, err = store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	j := value.Profiles[s.profileID].Connection.Journal
	for _, f := range j.Filters {
		if f.ExitOverlay != "" {
			t.Fatal("stale exit receipt", f.Key)
		}
	}
	if len(j.Routes) != 2 {
		t.Fatal("unexpected remaining routes", len(j.Routes))
	}
	for _, r := range j.Routes {
		if r.Resource && r.Destination != subnet {
			t.Fatal(r)
		}
	}
}

func TestSessionRefreshLostACKNeverResumesOldGrant(t *testing.T) {
	for failedAt := 1; ; failedAt++ {
		s, store, slot, m, _, dynamic, _, _, clock := wiredSessionFixture(t)
		if err := startSessionFixture(t, s, m); err != nil {
			t.Fatal(err)
		}
		clock.advance(time.Second)
		next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) { addedPeerSnapshot(t, snapshot) })
		slot.loseACKAt = slot.writes + failedAt
		err := s.Refresh(context.Background(), next)
		if err == nil && slot.writes < slot.loseACKAt {
			if failedAt == 1 {
				t.Fatal("operation performed no durable writes")
			}
			slot.loseACKAt = 0
			if err := s.Stop(cleanupContext(t)); err != nil {
				t.Fatal(err)
			}
			break
		}
		if !errors.Is(err, state.ErrUncertain) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
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
		c := value.Profiles[s.profileID].Connection
		if c.LastAppliedMap == nil || c.LastAppliedMap.PayloadHash != m.Cursor().PayloadHash {
			t.Fatal("cleanup lost the last confirmed map", failure)
		}
		if failure != "offline-commit" {
			if c.MapCursor.PayloadHash != next.Cursor().PayloadHash {
				t.Fatal("failed target was lost", failure)
			}
			if err := connectivity.RecordAppliedMap(t.Context(), store, s.generation, m); !errors.Is(err, state.ErrStale) {
				t.Fatal("late apply replaced the newer target", err)
			}
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
	for failedAt := 1; ; failedAt++ {
		s, _, slot, m, _, dynamic, _, _, _ := wiredSessionFixture(t)
		if err := startSessionFixture(t, s, m); err != nil {
			t.Fatal(err)
		}
		slot.loseACKAt = slot.writes + failedAt
		err := s.Offline(context.Background())
		if err == nil && slot.writes < slot.loseACKAt {
			if failedAt == 1 {
				t.Fatal("operation performed no durable writes")
			}
			slot.loseACKAt = 0
			if err := s.Stop(cleanupContext(t)); err != nil {
				t.Fatal(err)
			}
			break
		}
		if !errors.Is(err, state.ErrUncertain) || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
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
