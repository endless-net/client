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
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
	"github.com/tailscale/wf"
	"github.com/tailscale/wireguard-go/conn"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type sessionClock struct {
	mu     sync.Mutex
	sample connectivity.ClockSample
}

const sessionEpoch = "5e2c63ba-c810-4b04-af73-4c64ef0f24ab"

func (c *sessionClock) Sample() (connectivity.ClockSample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sample, nil
}
func (c *sessionClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sample.Wall = c.sample.Wall.Add(d)
	c.sample.Uptime += d
	c.sample.UpperUptime += d
}

type sessionAnnouncements struct{ value control.Announcement }

func (a sessionAnnouncements) Fetch(context.Context, string) (control.Announcement, error) {
	return a.value, nil
}

func sessionSignedFixtureAt(t *testing.T, now time.Time) (*state.Store, *cleanupSlot, *sessionClock, connectivity.Map) {
	t.Helper()
	bundle := func(seed byte) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32))
		public := key.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(public)
		id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
		before, after := now.Add(-time.Hour), now.Add(2*time.Hour)
		return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public), NotBefore: &before, NotAfter: &after}}}, key
	}
	mapBundle, mapKey := bundle(61)
	nodeBundle, nodeKey := bundle(62)
	relayBundle, _ := bundle(63)
	raw, err := json.Marshal(signing.ServerKeyResponse{TrustBundle: mapBundle, NodeCredentialTrustBundle: nodeBundle, RelayTrustBundle: relayBundle})
	if err != nil {
		t.Fatal(err)
	}
	trust, err := control.DecodeAnnouncement("https://control.example", raw, now)
	if err != nil {
		t.Fatal(err)
	}
	store, slot, profile := protectedCleanupStoreAt(t, nil, now, func(p *state.Profile) {
		p.Connection.AcceptedAt = now
		p.Connection.Deadline = now.Add(30 * time.Second)
		p.Connection.Request = &state.ConnectionRequest{InstanceID: sessionEpoch, Revision: 1, ClockEpoch: sessionEpoch, Uptime: time.Hour, UpperUptime: time.Hour}
		p.Trust = state.ServerTrust{KeyID: trust.KeyID, AnnouncementID: trust.ID, Bundle: trust.Bundle}
		scopes := []string{"node:register", "node:map", "node:endpoint"}
		payload, err := coordinator.NodeCredentialSigningPayload(nodeBundle.ActiveKeyID, p.Attempt.NetworkID, p.Attempt.NodeID, scopes, p.Attempt.CredentialEnds)
		if err != nil {
			t.Fatal(err)
		}
		claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: nodeBundle.ActiveKeyID, NetworkID: p.Attempt.NetworkID, NodeID: p.Attempt.NodeID, Scopes: scopes, ExpiresAt: p.Attempt.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(nodeKey, payload))}
		raw, err := json.Marshal(claims)
		if err != nil {
			t.Fatal(err)
		}
		p.Secrets.NodeCredential = "enc_" + base64.RawURLEncoding.EncodeToString(raw)
	})
	private, err := ecdh.X25519().NewPrivateKey(profile.Keys.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{64}, 32))
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Minute)
	snapshot := coordinator.NetworkMapSnapshot{Revision: coordinator.MapRevision{Network: 1}, Network: coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.70.0.0/24", Revision: 1}, Node: coordinator.Node{ID: "node", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), AssignedIP: "100.70.0.1", ApprovalState: "approved", Status: "online"}, Peers: []coordinator.Peer{{ID: "peer", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(peerKey.PublicKey().Bytes()), Endpoint: "192.0.2.1:51820", EndpointGeneration: 1, EndpointExpiresAt: &expires, AllowedIPs: []string{"100.70.0.2/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.70.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}}}}
	payload, err := coordinator.NetworkMapSigningPayload(snapshot, trust.KeyID, now, expires)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	snapshot.MapSignature = &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: trust.KeyID, IssuedAt: now, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(mapKey, payload))}
	event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + snapshot.MapSignature.PayloadHash, To: snapshot.Revision, Snapshot: &snapshot, ResultSignature: snapshot.MapSignature}
	raw, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	clock := &sessionClock{sample: connectivity.ClockSample{Wall: now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: sessionEpoch}}
	verifier, err := connectivity.NewVerifier(profile, connectivity.VerifierConfig{Now: func() time.Time { return now }, Clock: clock, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192, Announcements: sessionAnnouncements{trust}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := verifier.Verify(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return store, slot, clock, m
}

func wiredSessionFixture(t *testing.T) (*WindowsSession, *state.Store, *cleanupSlot, connectivity.Map, *mockIPAPI, *mockFilterAPI, *mockFilterAPI, *factoryTUN, *sessionClock) {
	return wiredSessionFixtureAt(t, time.Now().UTC())
}

func sessionFixtureOperation(t *testing.T, s *WindowsSession, parent context.Context, restore bool) (context.Context, *connectivity.OperationBudget) {
	t.Helper()
	value, err := s.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := value.Profiles[s.profileID]
	defer clearSessionKeys(value)
	r := p.Connection.Request
	receipt := connectivity.ClockSample{Wall: p.Connection.AcceptedAt, Epoch: r.ClockEpoch, Uptime: r.Uptime, UpperUptime: r.UpperUptime}
	if restore {
		receipt, err = s.clock.Sample()
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := connectivity.NewOperationBudget(s.clock, s.profileID, s.generation, receipt, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return connectivity.WithOperationBudget(parent, b), b
}

// Existing running-session tests inject a successful path decision after local
// Start. This is fixture orchestration, never evidence of a real peer handshake.
func startSessionFixture(t *testing.T, s *WindowsSession, m connectivity.Map) error {
	t.Helper()
	ctx, b := sessionFixtureOperation(t, s, context.Background(), false)
	if err := s.Start(ctx, m); err != nil {
		return err
	}
	return b.Complete()
}

func beginSessionDisconnect(value *state.State, p *state.Profile) (bool, error) {
	r := p.Connection.Request
	return p.Connection.BeginRequestedAttempt(state.IntentDisconnected, p.Connection.AcceptedAt.Add(time.Second), state.ConnectionRequest{InstanceID: sessionEpoch, Revision: value.Revision, ClockEpoch: sessionEpoch, Uptime: r.Uptime + time.Second, UpperUptime: r.UpperUptime + time.Second})
}

func wiredSessionFixtureAt(t *testing.T, now time.Time) (*WindowsSession, *state.Store, *cleanupSlot, connectivity.Map, *mockIPAPI, *mockFilterAPI, *mockFilterAPI, *factoryTUN, *sessionClock) {
	t.Helper()
	store, slot, clock, m := sessionSignedFixtureAt(t, now)
	resources, err := NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewWindowsSession(context.Background(), store, clock, cleanupProfile, 3, WindowsSessionConfig{MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, MaxInterfaces: 256, MaxInventoryRows: 256, MTU: 1420, ListenPort: 51820, RouteMetric: 7, DADPollInterval: time.Millisecond, TransactionWait: 100 * time.Millisecond, ApplyTimeout: time.Second, RelayDialTimeout: time.Second, RelayIOTimeout: 3 * time.Second, RelayHeartbeatInterval: time.Second, ProbeInterval: 10 * time.Millisecond, RelayRetryInterval: 20 * time.Millisecond}, resources)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := canonicalGUID(s.id)
	s.networkContext = func(context.Context, []byte) (string, error) { return "", nil }
	prepared, _ := PreparedWindowsAdapter(s.id)
	tunnel := &factoryTUN{inertTUN: newInertTUN(), luid: 12345}
	tunAPI := &mockTUNAPI{row: adapterIdentity{GUID: key, LUID: 12345, Index: 17, Name: prepared.Name, Type: 53}, device: tunnel}
	factory := &WindowsTUNFactory{api: tunAPI, load: func() error { return nil }, maxInterfaces: 256}
	ip := &mockIPAPI{key: key}
	s.createTUN, s.identity = factory.Create, tunAPI.identity
	s.setMTU = func(context.Context, state.AdapterRecord, int) error { return nil }
	s.ip = &WindowsIP{api: ip, maxRows: 256}
	dynamic, guards := &mockFilterAPI{next: 2000}, &mockFilterAPI{persistent: true, next: 1000}
	dynamic.layerOwner, guards.dynamicRef = guards, dynamic
	s.makeFilters = func(_ string, _ time.Duration, writer GuardJournalWriter) (*WindowsFilters, error) {
		return &WindowsFilters{api: dynamic, session: key, guards: &persistentGuards{api: guards, key: key}, writeGuardIDs: writer}, nil
	}
	s.planFilters = func(_ context.Context, m connectivity.Map, id string, luid uint64, index uint32, maxGrants, maxProtected int, offline bool) (FilterPlan, error) {
		snapshot, err := m.Snapshot()
		if err != nil {
			return FilterPlan{}, err
		}
		return filterPlan(snapshot, id, luid, index, maxGrants, maxProtected, offline)
	}
	s.makeBind = func() conn.Bind { return &inertBind{} }
	ip.afterCreate = func() {
		if guards.commits == 0 || len(guards.live) == 0 || s.lease.Permissions().ExistingFlows {
			t.Fatal("native IP reached Windows before guards or while packets enabled")
		}
		for i := range ip.addrs {
			ip.addrs[i].DadState = winipcfg.DadStatePreferred
		}
	}
	tunnel.afterClose = func() { tunAPI.rows = nil; ip.gone = true; ip.addrs = nil; ip.forward = nil }
	s.cleanup.adapter, s.cleanup.ip = factory, s.ip
	s.cleanup.guards = func(_ context.Context, j state.ResourceJournal) (recoveredGuards, error) {
		p, ids, err := recoverGuardPlan(j, 16)
		if err != nil {
			return nil, err
		}
		g := &persistentGuards{api: guards, key: p.session, ids: ids, rules: make(map[wf.RuleID]*wf.Rule), started: true}
		for _, r := range p.rules {
			g.rules[r.ID] = r
		}
		return &WindowsFilters{session: p.session, guards: g, attempted: &p, recoveryOnly: true, dynamicClosed: true}, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	})
	return s, store, slot, m, ip, dynamic, guards, tunnel, clock
}
