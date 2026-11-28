package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
)

func TestLinuxSignedMapStartsAndRevokesNativeSession(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	fixture := newCandidateFixture(t)
	fixture.snapshot.Peers = nil
	fixture.verify(t, nil)
	session, budget := linuxTestSession(t, fixture)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = connectivity.WithOperationBudget(ctx, budget)
	if err := session.Start(ctx, fixture.verified); err != nil {
		t.Fatal(err)
	}
	if err := session.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	if local := session.ConnectionLocal(); !local.Applied || !local.SoleNodeReady || local.Address != "100.70.0.1" || !local.Permission.NewFlows {
		t.Fatalf("unexpected Linux readiness: %#v", local)
	}
	for _, outage := range []time.Duration{time.Second, time.Minute} {
		original := session.ConnectionLocal().Permission
		epoch := session.BlockSource()
		if err := session.Offline(WithSourceEpoch(ctx, epoch)); err != nil {
			t.Fatal(err)
		}
		if offline := session.ConnectionLocal().Permission; offline.NewFlows || !offline.ExistingFlows || !offline.ExpiresAt.Equal(original.ExpiresAt) {
			t.Fatalf("offline changed the original lease: %#v", offline)
		}
		fixture.clock.advance(outage, outage)
		offline := session.ConnectionLocal().Permission
		if offline.NewFlows || offline.ExistingFlows != (outage < time.Minute) || !offline.ExpiresAt.Equal(original.ExpiresAt) {
			t.Fatalf("offline applied the wrong lease after %v: %#v", outage, offline)
		}
		previous := fixture.verified.Cursor()
		fixture.snapshot.Revision.Network++
		fixture.snapshot.Network.Revision = fixture.snapshot.Revision.Network
		fixture.verify(t, &previous)
		epoch = session.BlockSource()
		if err := session.Refresh(WithSourceEpoch(ctx, epoch), fixture.verified); err != nil {
			t.Fatal(err)
		}
		if local := session.ConnectionLocal(); !local.Permission.NewFlows || !local.Permission.ExistingFlows || local.MapRevision != fixture.snapshot.Revision.Network {
			t.Fatalf("fresh map did not restore packet authority after %v: %#v", outage, local)
		}
	}
	session.RevokeSource()
	if local := session.ConnectionLocal(); local.Permission.NewFlows || local.Permission.ExistingFlows {
		t.Fatal("revoked session retained packet authority")
	}
}

func linuxTestSession(t *testing.T, fixture *candidateFixture) (*LinuxSession, *connectivity.OperationBudget) {
	t.Helper()
	store, _ := hintStoreFixture(t, fixture)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return linuxTestSessionWithStore(t, fixture, store, directory)
}
func linuxTestSessionWithStore(t *testing.T, fixture *candidateFixture, store *state.Store, directory string) (*LinuxSession, *connectivity.OperationBudget) {
	t.Helper()
	ledger := state.NewLinuxNetworkLedger(directory)
	resources, err := NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := fixture.clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	budget, err := connectivity.NewOperationBudget(fixture.clock, fixture.profile.ID, 1, sample, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewLinuxSession(t.Context(), store, ledger, fixture.clock, fixture.profile.ID, 1, LinuxSessionConfig{
		MTU: 1420, MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, ListenPort: 51820,
		ProbeInterval: time.Second, RetryInterval: time.Second,
		Relay: RelayConfig{DialTimeout: time.Second, IOTimeout: 3 * time.Second, HeartbeatInterval: time.Second, Now: func() time.Time { return sample.Wall }},
	}, resources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := session.Stop(cleanup); err != nil {
			t.Error(err)
		}
	})
	return session, budget
}

func TestLinuxRouterForwardingIsOwnedAndReadBack(t *testing.T) {
	root := t.TempDir()
	const tunnel = "en0123456789ab"
	for _, name := range []string{tunnel, "default", "all", "eth0"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "forwarding"), []byte("0\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	owned := func() error { return nil }
	if err := linuxTunnelForwarding(root, tunnel, false, owned); !errors.Is(err, ErrIPConflict) {
		t.Fatal("disabled tunnel reported ready", err)
	}
	seen := 0
	lost := func() error {
		seen++
		if seen > 1 {
			return ErrAdapterOwnership
		}
		return nil
	}
	if err := linuxTunnelForwarding(root, tunnel, true, lost); !errors.Is(err, ErrAdapterOwnership) {
		t.Fatal("ownership changed before write", err)
	}
	path := filepath.Join(root, tunnel, "forwarding")
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "0\n" {
		t.Fatal("changed foreign link", err)
	}
	if err := linuxTunnelForwarding(root, tunnel, true, owned); err != nil {
		t.Fatal(err)
	}
	if err := linuxTunnelForwarding(root, tunnel, false, owned); err != nil {
		t.Fatal("owned link still unavailable", err)
	}
	for _, name := range []string{"default", "all", "eth0", "../" + tunnel} {
		if err := linuxTunnelForwarding(root, name, true, owned); !errors.Is(err, ErrAdapterOwnership) {
			t.Fatal("accepted unowned name", name, err)
		}
	}
	for _, name := range []string{"default", "all", "eth0"} {
		if raw, err := os.ReadFile(filepath.Join(root, name, "forwarding")); err != nil || string(raw) != "0\n" {
			t.Fatal("host forwarding changed", name, err)
		}
	}
	if err := os.WriteFile(path, []byte("unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := linuxTunnelForwarding(root, tunnel, true, owned); !errors.Is(err, ErrConfiguration) {
		t.Fatal("unknown sysctl admitted", err)
	}
}

func TestLinuxResourceFailoverUsesCurrentPeerEvidenceAndFencesQueue(t *testing.T) {
	f := newCandidateFixture(t)
	f.resourceRoutes = true
	first := &f.snapshot.Peers[0]
	first.ACLGrants = nil
	backup := *first
	key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{36}, 32))
	if err != nil {
		t.Fatal(err)
	}
	backup.ID, backup.PublicKey, backup.Endpoint, backup.AllowedIPs, backup.Transport = "backup", base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), "192.0.2.25:51820", []string{"100.70.0.3/32"}, nil
	f.snapshot.Peers = append(f.snapshot.Peers, backup)
	const target = "10.31.0.0/24"
	app := coordinator.Application{ID: "service", TargetType: "cidr", Target: target, AllowedPorts: []coordinator.ServicePort{{Protocol: "udp", Port: 443}}}
	for i, p := range f.snapshot.Peers {
		h := coordinator.ServiceHost{NodeID: p.ID, PublicKey: p.PublicKey}
		app.Connectors = append(app.Connectors, h)
		app.Routes = append(app.Routes, coordinator.ApplicationRoute{Connector: h, Metric: uint32(10 + i), CIDRs: []string{target}, ExpiresAt: *p.EndpointExpiresAt})
	}
	f.snapshot.Network.Applications = []coordinator.Application{app}
	prev := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.clock.advance(time.Millisecond, time.Millisecond)
	f.refresh(t, &prev)
	sw, _ := switchFixture(t, f)
	e, err := NewEngine(newInertTUN(), &inertBind{}, 8, 51820, f.lease)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Apply(t.Context(), f.verified, f.profile.Keys.WireGuardPrivate); err != nil {
		t.Fatal(err)
	}
	sample, _ := f.clock.Sample()
	status := func(id string, age time.Duration) *clientv0.PeerTransportStatus {
		return &clientv0.PeerTransportStatus{PeerId: id, SelectedPath: clientv0.PathKind_PATH_KIND_DIRECT, CheckedAt: timestamppb.New(sample.Wall.Add(-age))}
	}
	standby, _, err := recipientRulesWithHealth(f.snapshot, func(h coordinator.ServiceHost) bool { return h.NodeID == backup.ID })
	if err != nil || len(standby.forPeer(backup.ID)) != 1 {
		t.Fatal("standby could not prepare a cipher", err)
	}
	peers, err := permittedTransportPeers(f.snapshot)
	if err != nil || !peers[first.ID] || !peers[backup.ID] {
		t.Fatal("standby excluded from health checks", err)
	}
	oldEvidence := resourceObservations(f.snapshot, []*clientv0.PeerTransportStatus{status(backup.ID, 0)}, sample, nil)
	changed := f.snapshot
	changed.Peers = append([]coordinator.Peer(nil), f.snapshot.Peers...)
	changed.Peers[1].EndpointGeneration++
	if paths, err := observedResourcePaths(changed, oldEvidence, sample); err != nil || len(paths) != 0 {
		t.Fatal("old endpoint observation reused", err, paths)
	}
	raw := cipherIPv4(f.snapshot.Node.AssignedIP, "10.31.0.8", 1)
	if err := sw.send(raw); err != nil {
		t.Fatal(err)
	}
	old := nextSwitchPrimary(t, sw)
	for _, tc := range []struct {
		want     string
		statuses []*clientv0.PeerTransportStatus
	}{
		{first.ID, []*clientv0.PeerTransportStatus{status(backup.ID, 0), status(first.ID, 0)}},
		{backup.ID, []*clientv0.PeerTransportStatus{status(first.ID, 30*time.Second), status(backup.ID, 0)}},
		{"", []*clientv0.PeerTransportStatus{status(backup.ID, -time.Second)}},
		{first.ID, []*clientv0.PeerTransportStatus{status(first.ID, 0)}},
	} {
		live := resourceObservations(f.snapshot, tc.statuses, sample, nil)
		paths, err := observedResourcePaths(f.snapshot, live, sample)
		if err != nil {
			t.Fatal(err)
		}
		rules, _, err := recipientRulesForPaths(f.snapshot, paths)
		if err != nil {
			t.Fatal(err)
		}
		if err := sw.replaceResourceRules(f.verified, rules, func() error {
			if err := sw.send(raw); !errors.Is(err, ErrNoAuthority) {
				t.Fatal("packet during reassignment", err)
			}
			return e.applyPaths(t.Context(), f.snapshot, f.profile.Keys.WireGuardPrivate, paths)
		}); err != nil {
			t.Fatal(err)
		}
		rows := sw.observedResources(f.verified, f.snapshot)
		if len(rows) != 1 || rows[0].Id != app.ID || rows[0].GetSubnet().GetCidr() != target || (rows[0].Availability.Availability == clientv0.Availability_AVAILABILITY_AVAILABLE) != (tc.want != "") {
			t.Fatal("local resource status disagrees with installed path", rows, tc.want)
		}
		if actual := sw.resources.lookup(netip.MustParseAddr("10.31.0.8")).peer; actual != tc.want {
			t.Fatal("wrong selected path", actual, tc.want)
		}
		for _, p := range f.snapshot.Peers {
			if (e.bindings[p.PublicKey].resources == target) != (tc.want == p.ID) {
				t.Fatal("cryptographic route disagrees with switch", p.ID)
			}
		}
		if tc.want == backup.ID && old.guard() {
			t.Fatal("queued packet survived primary loss")
		}
		if err := sw.send(raw); tc.want == "" {
			if !errors.Is(err, ErrNoAuthority) {
				t.Fatal("both paths down", err)
			}
		} else if err != nil {
			t.Fatal(err)
		} else {
			_ = nextSwitchPrimary(t, sw)
		}
	}
	store, _ := hintStoreFixture(t, f)
	if err := connectivity.PersistCursor(t.Context(), store, 1, f.verified); err != nil {
		t.Fatal(err)
	}
	sup := &transportSupervisor{lease: f.lease, statusCursor: f.verified}
	session := &LinuxSession{engine: e, packets: sw, supervisor: sup, clock: f.clock, current: f.verified, store: store, profile: f.profile.ID, generation: 1, config: LinuxSessionConfig{ProbeInterval: time.Second}, policy: linuxFilterPlan{activeResources: []netip.Prefix{netip.MustParsePrefix(target)}}}
	// A near-expired backup is selected while the primary lacks proof.
	// A stalled wall clock and a repeated status cannot extend its remaining
	// second, even during the settling interval after switching AllowedIPs.
	sup.status = []*clientv0.PeerTransportStatus{status(backup.ID, 29*time.Second)}
	if err := session.reconcileResourcePaths(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := sw.resources.lookup(netip.MustParseAddr("10.31.0.8")).peer; got != backup.ID {
		t.Fatal("backup not selected", got)
	}
	f.clock.advance(0, time.Second)
	sup.status = append(sup.status, status(first.ID, 0))
	if err := session.reconcileResourcePaths(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := sw.resources.lookup(netip.MustParseAddr("10.31.0.8")).peer; got != first.ID {
		t.Fatal("settling retained expired backup", got)
	}
	for _, want := range []string{backup.ID, "", first.ID} {
		f.clock.advance(4*time.Second, 4*time.Second)
		sample, _ = f.clock.Sample()
		sup.status = []*clientv0.PeerTransportStatus{pendingPeerTransport(first.ID), pendingPeerTransport(backup.ID)}
		if want != "" {
			sup.status = append(sup.status, status(want, 0))
		}
		if err := session.reconcileResourcePaths(t.Context()); err != nil {
			t.Fatal("periodic session reconciliation", err)
		}
		if got := sw.resources.lookup(netip.MustParseAddr("10.31.0.8")).peer; got != want {
			t.Fatal("session selected", got, "want", want)
		}
	}

}
