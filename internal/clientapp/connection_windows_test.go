//go:build windows

package clientapp

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
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
)

type connectionAnnouncement struct{ value control.Announcement }

func (a connectionAnnouncement) Fetch(context.Context, string) (control.Announcement, error) {
	return a.value, nil
}

type connectionStreamBody struct {
	data   *bytes.Reader
	closed chan struct{}
	once   sync.Once
	ctx    context.Context
}

func (b *connectionStreamBody) Read(p []byte) (int, error) {
	if b.data.Len() != 0 {
		return b.data.Read(p)
	}
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.closed:
		return 0, io.EOF
	}
}
func (b *connectionStreamBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

type connectionNativeFixture struct {
	store                         *state.Store
	started, bound, revoked       atomic.Bool
	starts, stops, checks, epochs atomic.Uint64
	refreshes, offlines           atomic.Uint64
	probes                        atomic.Uint64
	wrongPort                     bool
	refreshError                  atomic.Pointer[error]
	refreshHook                   func(context.Context) error
	readyHook                     func() error
	advertisement                 atomic.Pointer[coordinator.TransportAdvertisement]
	advertisementChanged          chan struct{}
}

func (n *connectionNativeFixture) Start(ctx context.Context, m connectivity.Map) error {
	if err := connectivity.PersistCursor(ctx, n.store, 1, m); err != nil {
		return err
	}
	n.started.Store(true)
	n.bound.Store(true)
	n.starts.Add(1)
	return nil
}
func (n *connectionNativeFixture) Restore(ctx context.Context, m connectivity.Map) error {
	return n.Start(ctx, m)
}
func (n *connectionNativeFixture) Refresh(ctx context.Context, m connectivity.Map) error {
	if n.refreshHook != nil {
		if err := n.refreshHook(ctx); err != nil {
			return err
		}
	}
	err := connectivity.PersistCursor(ctx, n.store, 1, m)
	if err != nil {
		n.refreshError.Store(&err)
	}
	if err == nil {
		n.refreshes.Add(1)
	}
	return err
}
func (n *connectionNativeFixture) Offline(context.Context) error { n.offlines.Add(1); return nil }
func (n *connectionNativeFixture) Stop(context.Context) error {
	n.bound.Store(false)
	n.stops.Add(1)
	return nil
}
func (n *connectionNativeFixture) CheckReady(ctx context.Context) error {
	value, err := n.store.Load(ctx)
	if err != nil {
		return err
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if !n.started.Load() || p.Connection.EndpointRequest == nil || p.Connection.EndpointGeneration == 1 && p.Connection.EndpointRequest.Receipt == nil {
		return errors.New("readiness check before durable endpoint ACK")
	}
	n.checks.Add(1)
	if n.readyHook != nil {
		return n.readyHook()
	}
	return nil
}
func (n *connectionNativeFixture) ProbePaths(context.Context) error { n.probes.Add(1); return nil }
func (n *connectionNativeFixture) BoundPort() (uint16, error) {
	if !n.bound.Load() {
		return 0, dataplane.ErrClosed
	}
	if n.wrongPort {
		return 51821, nil
	}
	return 51820, nil
}

func (n *connectionNativeFixture) TransportAdvertisement(netip.Addr) (coordinator.TransportAdvertisement, error) {
	if ad := n.advertisement.Load(); ad != nil {
		return *ad, nil
	}
	return coordinator.TransportAdvertisement{IntentGeneration: 1, Capabilities: dataplane.TransportCapabilities(), Endpoints: []coordinator.TransportEndpoint{}}, nil
}

func (n *connectionNativeFixture) AdvertisementSignal() <-chan struct{} {
	return n.advertisementChanged
}
func (n *connectionNativeFixture) MapEpoch(connectivity.Map) uint64 { return n.epochs.Add(1) }
func (n *connectionNativeFixture) BlockSource() uint64              { return n.epochs.Add(1) }
func (n *connectionNativeFixture) RevokeSource()                    { n.revoked.Store(true) }

func (n *connectionNativeFixture) ConnectionLocal() connectivity.LocalObservation {
	return connectivity.LocalObservation{Known: n.started.Load() && !n.revoked.Load(), Applied: n.started.Load(), PeerCount: 1, Permission: connectivity.Permission{NewFlows: true}}
}

type endpointClockFunc func() (connectivity.ClockSample, error)

func (f endpointClockFunc) Sample() (connectivity.ClockSample, error) { return f() }

func TestWindowsConnectionRecoveryRefreshesHistoricalEndpointACKBeforeReady(t *testing.T) {
	for _, mode := range []string{"expired", "changed", "current", "prefix_withdrawn", "capability_changed", "uncertain_next", "expired_next", "budget"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectionBackendFixture(t)
			f.config.EndpointTTL = 120 * time.Second
			f.config.RenewalInterval = time.Minute
			if mode == "budget" {
				var jumped atomic.Bool
				f.config.Verifier.Clock = endpointClockFunc(func() (connectivity.ClockSample, error) {
					sample, err := (workerFixtureClock{}).Sample()
					if jumped.Load() {
						sample.Wall = sample.Wall.Add(31 * time.Second)
						sample.Uptime += 31 * time.Second
						sample.UpperUptime += 31 * time.Second
					}
					return sample, err
				})
				f.afterACK = func(coordinator.UpdateNodeEndpointResponse) { jumped.Store(true) }
			}
			f.factory.config = f.config.shared()
			endpoint := f.config.Endpoint.String()
			if mode == "changed" {
				endpoint = "192.0.2.99:51821"
			}
			input := coordinator.UpdateNodeEndpointRequest{Endpoint: endpoint, Generation: 1, TTL: f.config.EndpointTTL.String(), Status: "online"}
			if mode == "prefix_withdrawn" {
				input.AdvertisedCIDRs = []string{"10.20.0.0/24"}
			}
			if mode == "current" || mode == "prefix_withdrawn" {
				ad, err := f.native.TransportAdvertisement(f.config.Endpoint.Addr())
				if err != nil {
					t.Fatal(err)
				}
				input.Transport = &ad
			}
			if err := connectivity.PrepareEndpoint(context.Background(), f.store, workerProfile, 1, input); err != nil {
				t.Fatal(err)
			}
			value, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer clearConnectionKeys(value)
			// The old process persisted Sent, committed remotely and lost the ACK.
			if err := f.store.Update(context.Background(), value.Revision, func(next *state.State) error {
				p := next.Profiles[workerProfile]
				p.Connection.EndpointRequest.Sent = true
				next.Profiles[workerProfile] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(value.Profiles[workerProfile].Connection.EndpointRequest.Request)
			f.originals[1] = original
			updated := time.Now().UTC().Add(-time.Second)
			if mode != "current" && mode != "changed" && mode != "capability_changed" && mode != "prefix_withdrawn" {
				updated = updated.Add(-120 * time.Second)
			}
			f.acks[1] = coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: endpoint, Generation: 1, Online: true, UpdatedAt: updated, ExpiresAt: updated.Add(f.config.EndpointTTL), Revision: coordinator.MapRevision{Network: 1}}
			ack := f.acks[1]
			ack.Transport = input.Transport
			f.acks[1] = ack
			if mode == "expired_next" {
				ack := f.acks[1]
				ack.Generation = 2
				f.acks[2] = ack
			}
			f.loseSecond = mode == "uncertain_next"
			owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
			if err != nil {
				t.Fatal(err)
			}
			d := owner.(*connectionDriver)
			defer func() {
				if err := d.Disconnect(workerCallContext(t)); err != nil {
					t.Error(err)
				}
			}()
			err = d.Connect(backendOperation(t, f), false)
			if mode == "budget" {
				if !errors.Is(err, context.DeadlineExceeded) || f.native.checks.Load() != 0 {
					t.Fatal("expired native budget reached next announcement or Direct", err)
				}
			} else if mode == "uncertain_next" || mode == "expired_next" {
				if !errors.Is(err, connectivity.ErrEndpointUncertain) || f.native.checks.Load() != 0 {
					t.Fatal("unconfirmed current announcement reached Direct", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer clearConnectionKeys(after)
			p := after.Profiles[workerProfile]
			if mode == "prefix_withdrawn" {
				current, err := coordinator.DecodeUpdateNodeEndpointRequest(p.Connection.EndpointRequest.Request, len(p.Connection.EndpointRequest.Request))
				if err != nil || len(current.AdvertisedCIDRs) != 0 {
					t.Fatal("new generation retained withdrawn prefix", current, err)
				}
			}
			want := uint64(2)
			if mode == "current" || mode == "budget" {
				want = 1
			}
			if p.Connection.Generation != 1 || p.Connection.EndpointGeneration != want || !bytes.Equal(f.originals[1], original) || p.Connection.Deadline != value.Profiles[workerProfile].Connection.Deadline {
				t.Fatal("historical receipt used as current lease, frozen request changed or operation renewed", p.Connection.EndpointGeneration, want)
			}
			if mode != "uncertain_next" && mode != "expired_next" && mode != "budget" {
				if receipt := p.Connection.EndpointRequest.Receipt; receipt == nil || receipt.Endpoint != f.config.Endpoint.String() || !receipt.ExpiresAt.After(time.Now()) || f.native.checks.Load() != 1 {
					t.Fatal("Direct admitted without a current matching discovery ACK")
				}
			}
		})
	}
}

type connectionBackendFixture struct {
	dnsConfig    *coordinator.DNSConfig
	backend      *workerMemory
	now          func() time.Time
	checkRequest func(*http.Request)
	store        *state.Store
	native       *connectionNativeFixture
	factory      *connectionFactory
	config       WindowsConnectionConfig
	transport    *connectivity.Transport
	mu           sync.Mutex
	requests     []coordinator.UpdateNodeEndpointRequest
	originals    map[uint64][]byte
	acks         map[uint64]coordinator.UpdateNodeEndpointResponse
	loseSecond   bool
	lost         bool
	patches      chan uint64
	streamBody   *connectionStreamBody
	afterACK     func(coordinator.UpdateNodeEndpointResponse)
}

func newConnectionBackendFixture(t *testing.T) *connectionBackendFixture {
	return newConnectionBackendFixturePrepared(t, nil, time.Second)
}

func newConnectionBackendFixturePrepared(t *testing.T, prepare func(*state.Profile), idle time.Duration) *connectionBackendFixture {
	t.Helper()
	fixture := &connectionBackendFixture{originals: map[uint64][]byte{}, acks: map[uint64]coordinator.UpdateNodeEndpointResponse{}, patches: make(chan uint64, 32)}
	fixture.now = time.Now
	now := time.Now().UTC()
	bundle := func(seed byte) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32))
		pub := key.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(pub)
		id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
		before, after := now.Add(-time.Hour), now.Add(24*time.Hour)
		return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(pub), NotBefore: &before, NotAfter: &after}}}, key
	}
	mapBundle, mapKey := bundle(61)
	nodeBundle, nodeKey := bundle(62)
	relayBundle, _ := bundle(63)
	raw, err := json.Marshal(signing.ServerKeyResponse{TrustBundle: mapBundle, NodeCredentialTrustBundle: nodeBundle, RelayTrustBundle: relayBundle})
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := control.DecodeAnnouncement("https://control.example", raw, now)
	if err != nil {
		t.Fatal(err)
	}
	fixture.store, fixture.backend = workerStorePrepared(t, func(p *state.Profile) {
		if prepare != nil {
			prepare(p)
		}
		p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
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
	if err := workerIntent(t, fixture.store, state.IntentConnected, now, workerRuntime); err != nil {
		t.Fatal(err)
	}
	fixture.native = &connectionNativeFixture{store: fixture.store}
	var revision atomic.Uint64
	trip := roundTrip(func(request *http.Request) (*http.Response, error) {
		if fixture.checkRequest != nil {
			fixture.checkRequest(request)
		}
		if request.URL.Path == publicqueryv1connect.ConnectivityServiceUpdateNodeEndpointProcedure {
			if !fixture.native.bound.Load() {
				return nil, errors.New("announcement before socket receipt")
			}
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			input, err := testwire.EndpointRequest(raw)
			if err != nil {
				return nil, err
			}
			raw, err = json.Marshal(input)
			if err != nil {
				return nil, err
			}
			fixture.mu.Lock()
			fixture.requests = append(fixture.requests, input)
			if original, exists := fixture.originals[input.Generation]; exists && !bytes.Equal(original, raw) {
				fixture.mu.Unlock()
				return nil, errors.New("changed replay body")
			}
			fixture.originals[input.Generation] = bytes.Clone(raw)
			ack, exists := fixture.acks[input.Generation]
			if !exists {
				updated := fixture.now().UTC()
				ttl, _ := time.ParseDuration(input.TTL)
				ack = coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: input.Endpoint, Generation: input.Generation, Online: input.Status == "online", UpdatedAt: updated, ExpiresAt: updated.Add(ttl), Revision: coordinator.MapRevision{Network: revision.Load()}}
				fixture.acks[input.Generation] = ack
			}
			ack.Transport = input.Transport
			fixture.acks[input.Generation] = ack
			lose := fixture.loseSecond && input.Generation == 2 && !fixture.lost
			if lose {
				fixture.lost = true
			}
			fixture.mu.Unlock()
			select {
			case fixture.patches <- input.Generation:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			if lose {
				return nil, errors.New("lost endpoint ACK")
			}
			if fixture.afterACK != nil {
				fixture.afterACK(ack)
			}
			return testwire.UnaryResponse(request, testwire.EndpointReceipt(ack))
		}
		watch, err := testwire.ReadWatch(request)
		if err != nil || request.URL.Path != publicqueryv1connect.ConnectivityServiceWatchMapsProcedure || watch.NodeId != "node" || watch.Cursor != nil {
			return nil, errors.New("not an explicit full snapshot request")
		}
		value, err := fixture.store.Load(request.Context())
		if err != nil {
			return nil, err
		}
		defer clearConnectionKeys(value)
		p := value.Profiles[workerProfile]
		private, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			return nil, err
		}
		peer, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{12}, 32))
		issued := fixture.now().UTC()
		expires := issued.Add(time.Minute)
		version := revision.Add(1)
		m := coordinator.NetworkMapSnapshot{Revision: coordinator.MapRevision{Network: version}, Network: coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.64.0.0/24", Revision: version}, Node: coordinator.Node{ID: "node", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), AssignedIP: "100.64.0.1", ApprovalState: "approved", Status: "online"}, Peers: []coordinator.Peer{{ID: "peer", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(peer.PublicKey().Bytes()), Endpoint: "192.0.2.2:51820", EndpointGeneration: 1, EndpointExpiresAt: &expires, AllowedIPs: []string{"100.64.0.2/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}}}}
		m.Network.DNSConfig = fixture.dnsConfig
		payload, err := coordinator.NetworkMapSigningPayload(m, mapBundle.ActiveKeyID, issued, expires)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(payload)
		m.MapSignature = &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: mapBundle.ActiveKeyID, IssuedAt: issued, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(mapKey, payload))}
		event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + m.MapSignature.PayloadHash, To: m.Revision, Snapshot: &m, ResultSignature: m.MapSignature}
		proof := coordinator.MapTimeProof{Nonce: watch.Nonce, MapHash: m.MapSignature.PayloadHash, ServerTime: issued}
		message, err := coordinator.MapTimeProofSigningPayload(proof, *m.MapSignature)
		if err != nil {
			return nil, err
		}
		proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(mapKey, message))
		event.TimeProof = &proof
		body, err := testwire.Frame(testwire.Update(event))
		if err != nil {
			return nil, err
		}
		stream := &connectionStreamBody{data: bytes.NewReader(body), closed: make(chan struct{}), ctx: request.Context()}
		fixture.mu.Lock()
		fixture.streamBody = stream
		fixture.mu.Unlock()
		return testwire.StreamResponse(request, stream), nil
	})
	transport, err := connectivity.NewTransport(&http.Client{Transport: trip}, connectivity.HTTPConfig{CallTimeout: time.Second, StreamDuration: 2 * idle, ReadIdleTimeout: idle, MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 8, MaxCalls: 1, MaxStreams: 1})
	if err != nil {
		t.Fatal(err)
	}
	fixture.transport = transport
	fixture.config = WindowsConnectionConfig{Verifier: connectivity.VerifierConfig{Announcements: connectionAnnouncement{announcement}, Now: time.Now, Clock: workerFixtureClock{}, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192}, Native: dataplane.WindowsSessionConfig{MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, MaxInventoryRows: 256, MaxInterfaces: 256, MTU: 1420, ListenPort: 51820, RouteMetric: 7, DADPollInterval: time.Millisecond, TransactionWait: 100 * time.Millisecond, ApplyTimeout: time.Second, RelayDialTimeout: time.Second, RelayIOTimeout: 3 * time.Second, RelayHeartbeatInterval: time.Second, ProbeInterval: 10 * time.Millisecond, RelayRetryInterval: 20 * time.Millisecond}, Endpoint: netip.MustParseAddrPort("192.0.2.1:51820"), EndpointTTL: 200 * time.Millisecond, RenewalInterval: 20 * time.Millisecond, RetryInterval: 20 * time.Millisecond, ProbeInterval: 10 * time.Millisecond, WriteBudget: time.Second, MaxQueuedMaps: 8}
	fixture.factory = &connectionFactory{store: fixture.store, transport: transport, config: fixture.config.shared(), stream: transport.StreamSnapshot, cleanup: func(context.Context, string) error { return nil }, makeNative: func(context.Context, string, uint64) (nativeConnectionSession, error) { return fixture.native, nil }}
	return fixture
}

func backendOperation(t *testing.T, f *connectionBackendFixture) context.Context {
	t.Helper()
	value, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	r := p.Connection.Request
	b, err := connectivity.NewOperationBudget(f.config.Verifier.Clock, workerProfile, 1, connectivity.ClockSample{Wall: p.Connection.AcceptedAt, Epoch: r.ClockEpoch, Uptime: r.Uptime, UpperUptime: r.UpperUptime}, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return connectivity.WithOperationBudget(ctx, b)
}

func TestInstalledWindowsConnectionAcceptsManagedDNS(t *testing.T) {
	f := newConnectionBackendFixture(t)
	v := f.config.Verifier
	var settings Config
	settings.CallTimeout = v.CallTimeout
	settings.MaxCredentialBytes = v.MaxCredentialBytes
	settings.MaxMapBytes = v.MaxBytes
	settings.MaxPeers = v.MaxPeers
	settings.MaxRules = v.MaxRules
	f.factory.config.Verifier = settings.connectionDriver(v.Clock, v.Announcements).shared().Verifier
	f.dnsConfig = &coordinator.DNSConfig{Zones: []coordinator.DNSZone{{
		Name: "zone.test.", Version: 1,
		Records: []coordinator.DNSRecord{{Name: "db.zone.test.", Type: "A", Value: "192.0.2.41", TTL: 19}},
	}}}
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() { _ = d.Disconnect(workerCallContext(t)) })
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	if f.native.starts.Load() != 1 {
		t.Fatal("signed map with a local zone did not reach the native session")
	}
}

func TestWindowsConnectionDriverCompletesAfterACKWithNoPeerPaths(t *testing.T) {
	f := newConnectionBackendFixture(t)
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() {
		if err := d.Disconnect(workerCallContext(t)); err != nil {
			t.Error(err)
		}
	})
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	value, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if p.Connection.MapCursor == nil || p.Connection.EndpointGeneration < 1 || f.native.starts.Load() != 1 || f.native.checks.Load() != 1 {
		t.Fatal("driver skipped actual SDK/state stages")
	}
	if p.Attempt.NodeID != "node" || p.Connection.Generation != 1 {
		t.Fatal("connect changed registration/generation")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for f.native.probes.Load() < 2 {
		select {
		case <-d.done:
			t.Fatal("zero peer paths ended the session", d.result())
		case <-deadline.C:
			t.Fatal("background peer probes did not run")
		case <-tick.C:
		}
	}
}

func TestPreparedListenerSignalPublishesBeforePeriodicRenewal(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.factory.config.EndpointTTL = 120 * time.Second
	f.factory.config.RenewalInterval = time.Minute
	f.native.advertisementChanged = make(chan struct{}, 1)
	owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() { _ = d.Disconnect(workerCallContext(t)) })
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	if err := d.refreshEndpoint(f.native, publishChangedEndpoint); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	announcements := len(f.requests)
	f.mu.Unlock()
	if announcements != 1 {
		t.Fatalf("unchanged listener generated %d endpoint updates, want 1", announcements)
	}
	ad, err := f.native.TransportAdvertisement(f.config.Endpoint.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ad.Endpoints = []coordinator.TransportEndpoint{{ProfileID: "prepared", ProfileDigest: strings.Repeat("1", 64), Endpoint: "192.0.2.1:51821"}}
	f.native.advertisement.Store(&ad)
	f.native.advertisementChanged <- struct{}{}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case generation := <-f.patches:
			if generation < 2 {
				continue
			}
			f.mu.Lock()
			request := f.requests[len(f.requests)-1]
			f.mu.Unlock()
			if request.Transport == nil || len(request.Transport.Endpoints) != 1 || request.Transport.Endpoints[0] != ad.Endpoints[0] {
				t.Fatal("listener update lost actual profile/port binding")
			}
			return
		case <-deadline.C:
			t.Fatal("listener waited for periodic renewal")
		}
	}
}

func TestWindowsConnectionDriverRenewalReplaysLostACKWithoutNewGeneration(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.loseSecond = true
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() {
		if err := d.Disconnect(workerCallContext(t)); err != nil {
			t.Error(err)
		}
	})
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	ctx := workerCallContext(t)
	for _, want := range []uint64{1, 2, 2} {
		select {
		case got := <-f.patches:
			if got != want {
				t.Fatal("renewal changed unresolved request generation", got, want)
			}
		case <-ctx.Done():
			t.Fatal("renewal/recovery did not run")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.lost || len(f.originals) < 2 || len(f.requests) < 3 || f.requests[1].Generation != 2 || f.requests[2].Generation != 2 {
		t.Fatal("recovery allocated a new request")
	}
}

func TestWindowsConnectionDriverReconnectsWithFullSnapshotWithoutResettingCursor(t *testing.T) {
	f := newConnectionBackendFixture(t)
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() {
		if err := d.Disconnect(workerCallContext(t)); err != nil {
			t.Error(err)
		}
	})
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	body := f.streamBody
	f.mu.Unlock()
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := workerCallContext(t)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for f.native.refreshes.Load() == 0 {
		select {
		case <-ctx.Done():
			if cause := f.native.refreshError.Load(); cause != nil {
				t.Fatal("source refresh failed", *cause)
			}
			t.Fatalf("source did not reconnect/apply full snapshot: %T %#v offline=%d revoked=%v", d.result(), d.result(), f.native.offlines.Load(), f.native.revoked.Load())
		case <-tick.C:
		}
	}
	value, err := f.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if p.Connection.MapCursor == nil || p.Connection.MapCursor.Revision.Network < 2 || p.Connection.Generation != 1 || p.Attempt.NodeID != "node" || f.native.starts.Load() != 1 || f.native.offlines.Load() != 1 || f.native.revoked.Load() {
		t.Fatal("reconnect reset identity/cursor or retried native creation")
	}
}

func TestAdmittedRefreshSurvivesScheduledStreamRotation(t *testing.T) {
	f := newConnectionBackendFixture(t)
	connected := make(chan struct{})
	entered := make(chan struct{})
	rotated := make(chan struct{})
	result := make(chan error, 1)
	f.native.refreshHook = func(ctx context.Context) error {
		close(entered)
		<-rotated
		select {
		case <-ctx.Done():
			result <- ctx.Err()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			result <- nil
			return nil
		}
	}
	stream := f.factory.stream
	first := true
	f.factory.stream = func(ctx context.Context, v *connectivity.Verifier, cursor *state.MapCursor, receive func(context.Context, connectivity.Map) error) error {
		if first {
			first = false
			return stream(ctx, v, cursor, func(ctx context.Context, m connectivity.Map) error {
				if err := receive(ctx, m); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-connected:
				}
				return connectivity.ErrStreamRotation
			})
		}
		return stream(ctx, v, cursor, func(ctx context.Context, m connectivity.Map) error {
			frame, cancel := context.WithCancelCause(ctx)
			defer cancel(nil)
			if err := receive(frame, m); err != nil {
				return err
			}
			select {
			case <-entered:
			case <-ctx.Done():
				return ctx.Err()
			}
			cancel(connectivity.ErrStreamRotation)
			close(rotated)
			<-ctx.Done()
			return ctx.Err()
		})
	}
	owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() { _ = d.Disconnect(workerCallContext(t)) })
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	close(connected)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("rotation interrupted admitted refresh:", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("refresh did not run")
	}
}

func TestWindowsConnectionDriverRotatesMapStreamWithoutEnteringOffline(t *testing.T) {
	for _, name := range []string{"replacement-works", "replacement-unavailable"} {
		t.Run(name, func(t *testing.T) {
			f := newConnectionBackendFixture(t)
			f.factory.config.RetryInterval = 30 * time.Second
			f.factory.config.RenewalInterval = time.Minute
			rotate := make(chan struct{})
			replacing := make(chan struct{})
			allowReplacement := make(chan struct{})
			stream := f.factory.stream
			calls := 0
			f.factory.stream = func(ctx context.Context, v *connectivity.Verifier, cursor *state.MapCursor, receive func(context.Context, connectivity.Map) error) error {
				calls++
				if calls == 1 {
					return stream(ctx, v, cursor, func(ctx context.Context, m connectivity.Map) error {
						if err := receive(ctx, m); err != nil {
							return err
						}
						select {
						case <-rotate:
							return connectivity.ErrStreamRotation
						case <-ctx.Done():
							return ctx.Err()
						}
					})
				}
				close(replacing)
				select {
				case <-allowReplacement:
				case <-ctx.Done():
					return ctx.Err()
				}
				if name == "replacement-unavailable" {
					return connectivity.ErrUnavailable
				}
				return stream(ctx, v, cursor, receive)
			}
			owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
			if err != nil {
				t.Fatal(err)
			}
			d := owner.(*connectionDriver)
			t.Cleanup(func() { _ = d.Disconnect(workerCallContext(t)) })
			if err := d.Connect(backendOperation(t, f), false); err != nil {
				t.Fatal(err)
			}
			close(rotate)
			ctx := workerCallContext(t)
			select {
			case <-replacing:
			case <-ctx.Done():
				t.Fatal("scheduled rotation waited for the outage retry interval")
			}
			if f.native.epochs.Load() != 1 || f.native.offlines.Load() != 0 || f.native.revoked.Load() {
				t.Fatal("scheduled rotation degraded the current source while opening its replacement")
			}
			close(allowReplacement)
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for f.native.refreshes.Load() == 0 && f.native.offlines.Load() == 0 {
				select {
				case <-ctx.Done():
					t.Fatal("replacement result never reached the native owner")
				case <-tick.C:
				}
			}
			wantOffline, wantRefresh := uint64(0), uint64(1)
			if name == "replacement-unavailable" {
				wantOffline, wantRefresh = 1, 0
			}
			if f.native.offlines.Load() != wantOffline || f.native.refreshes.Load() != wantRefresh ||
				f.native.epochs.Load() != 2 || f.native.starts.Load() != 1 || f.native.revoked.Load() {
				t.Fatal("replacement failed to preserve the owner or select its expected source policy")
			}
		})
	}
}

func TestWindowsConnectionDriverRejectsBindMismatchBeforeAnnouncementOrReadiness(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.native.wrongPort = true
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	defer func() {
		if err := d.Disconnect(workerCallContext(t)); err != nil {
			t.Error(err)
		}
	}()
	if err := d.Connect(backendOperation(t, f), false); !errors.Is(err, connectivity.FailureApply) {
		t.Fatal("wrong listener accepted", err)
	}
	value, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	if len(f.patches) != 0 || f.native.checks.Load() != 0 || value.Profiles[workerProfile].Connection.EndpointGeneration != 0 {
		t.Fatal("unconfirmed bind published or completed connection")
	}
}

func TestWindowsConnectionDriverQueueOverflowRevokesWithoutBlocking(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.factory.config.MaxQueuedMaps = 1
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	defer d.cancel()
	renewal := connectionFrame{ctx: t.Context(), epoch: 1}
	for range 3 {
		if err := d.enqueue(f.native, renewal); err != nil {
			t.Fatal(err)
		}
	}
	if pending := <-d.frames; pending.epoch != renewal.epoch || f.native.revoked.Load() {
		t.Fatal("renewal burst revoked the active tunnel")
	}
	if err := d.enqueue(f.native, connectionFrame{}); err != nil {
		t.Fatal(err)
	}
	if err := d.enqueue(f.native, connectionFrame{}); !errors.Is(err, ErrConnectionWorker) || !f.native.revoked.Load() {
		t.Fatal("bounded source queue dropped frame without revoke", err)
	}
}

func TestTerminalSourceFailurePurgesHintsButOldGenerationCannotPurgeNewIntent(t *testing.T) {
	f := newConnectionBackendFixture(t)
	value, err := f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	local, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{94}, 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hint := state.TransportHint{Key: state.TransportHintKey{LocalProfileID: workerProfile, AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, LocalNodeID: p.Attempt.NodeID, PeerNodeID: "peer", LocalPublicKey: base64.StdEncoding.EncodeToString(local.PublicKey().Bytes()), PeerPublicKey: base64.StdEncoding.EncodeToString(peer.PublicKey().Bytes()), ContextDigest: strings.Repeat("1", 64), Engine: "wireguard", ProtocolVersion: "1", TransportProfileID: "profile", ProfileVersion: 1, ProfileDigest: strings.Repeat("2", 64)}, SucceededAt: now, ExpiresAt: now.Add(state.TransportHintTTL), StableSpan: 5 * time.Second, SuccessfulProbes: 3}
	if err := f.store.Update(t.Context(), value.Revision, func(next *state.State) error { return next.RecordTransportSuccess(hint, now) }); err != nil {
		t.Fatal(err)
	}
	owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	defer d.cancel()
	d.failSource(f.native, connectivity.ErrRevoked)
	if err := d.applyMaps(t.Context(), false, f.native); !errors.Is(err, connectivity.ErrRevoked) {
		t.Fatal("terminal source outcome changed", err)
	}
	value, err = f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	if len(value.TransportHints) != 0 || !f.native.revoked.Load() {
		t.Fatal("terminal source left cached priority")
	}
	if err := f.store.Update(t.Context(), value.Revision, func(next *state.State) error {
		if err := next.RecordTransportSuccess(hint, now); err != nil {
			return err
		}
		p := next.Profiles[workerProfile]
		_, err := p.Connection.BeginRequestedAttempt(state.IntentDisconnected, now, state.ConnectionRequest{InstanceID: p.Connection.Request.InstanceID, Revision: next.Revision})
		next.Profiles[workerProfile] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d.invalidateTransportHints()
	value, err = f.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(value)
	if len(value.TransportHints) != 1 || value.Profiles[workerProfile].Connection.Generation != 2 {
		t.Fatal("late generation purged a later intent's metadata")
	}
}

func TestWindowsConnectionFactoryAllocatesNoNativeResourcesOrStateOnConstruction(t *testing.T) {
	f := newConnectionBackendFixture(t)
	before, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(before)
	factory, err := NewWindowsConnectionFactory(f.store, f.transport, f.config)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := factory(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Disconnect(workerCallContext(t)); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clearConnectionKeys(after)
	if after.Revision != before.Revision || after.Profiles[workerProfile].Connection.Journal != nil || len(f.patches) != 0 {
		t.Fatal("pure owner construction created side effects")
	}
}

func TestWindowsConnectionCompletesStartupAfterMapIsSupersededAtACK(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.factory.config.RenewalInterval = time.Minute
	ackStarted := make(chan struct{})
	ackWait := time.NewTimer(time.Second)
	defer ackWait.Stop()
	replacementQueued := make(chan struct{})
	f.afterACK = func(coordinator.UpdateNodeEndpointResponse) {
		close(ackStarted)
		select {
		case <-replacementQueued:
		case <-ackWait.C:
		}
	}

	stream := f.factory.stream
	f.factory.stream = func(ctx context.Context, v *connectivity.Verifier, cursor *state.MapCursor, receive func(context.Context, connectivity.Map) error) error {
		return stream(ctx, v, cursor, func(ctx context.Context, m connectivity.Map) error {
			if err := receive(ctx, m); err != nil {
				return err
			}
			if m.Cursor().Revision.Network > 1 {
				close(replacementQueued)
				return nil
			}
			select {
			case <-ackStarted:
				return connectivity.ErrStreamRotation
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	f.native.readyHook = func() error {
		if f.native.epochs.Load() == 2 && f.native.refreshes.Load() == 0 {
			return connectivity.ErrSourceChanged
		}
		return nil
	}
	operation := backendOperation(t, f)
	budget, err := connectivity.OperationFromContext(operation)
	if err != nil {
		t.Fatal(err)
	}
	f.native.refreshHook = func(ctx context.Context) error {
		current, err := connectivity.OperationFromContext(ctx)
		if err != nil || current != budget {
			return errors.New("refresh lost the startup budget")
		}
		return nil
	}
	owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Disconnect(workerCallContext(t)) }()
	if err := owner.Connect(operation, false); err != nil {
		t.Fatal(err)
	}
	if f.native.starts.Load() != 1 || f.native.refreshes.Load() != 1 ||
		f.native.checks.Load() != 2 || f.native.revoked.Load() {
		t.Fatal("expected readiness only after processing the replacement map in the original session")
	}
}

func TestEndpointRefreshReportsLostACKUntilExactReplaySucceeds(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.loseSecond = true
	f.factory.config.RenewalInterval = time.Minute
	reports := make(chan error, 2)
	f.factory.config.ReportFailure = func(_ uint64, stage string, err error) {
		if stage == "endpoint_refresh" {
			reports <- err
		}
	}
	owner, err := f.factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*connectionDriver)
	t.Cleanup(func() { _ = d.Disconnect(workerCallContext(t)) })
	if err := d.Connect(backendOperation(t, f), false); err != nil {
		t.Fatal(err)
	}
	if err := d.refreshEndpoint(f.native, renewEndpoint); err != nil || !d.ConnectionLocal().EndpointRefreshUnconfirmed {
		t.Fatal("lost endpoint ACK did not degrade discovery", err)
	}
	select {
	case err := <-reports:
		if !errors.Is(err, connectivity.ErrEndpointUncertain) {
			t.Fatal("lost endpoint ACK was misclassified", err)
		}
	default:
		t.Fatal("lost endpoint ACK produced no diagnostic")
	}
	d.credentialMu.Lock()
	err = d.refreshEndpoint(f.native, publishChangedEndpoint)
	d.credentialMu.Unlock()
	if err != nil || !d.ConnectionLocal().EndpointRefreshUnconfirmed {
		t.Fatal("skipped refresh cleared an unresolved endpoint acknowledgement", err)
	}
	if err := d.refreshEndpoint(f.native, publishChangedEndpoint); err != nil || d.ConnectionLocal().EndpointRefreshUnconfirmed {
		t.Fatal("successful exact replay did not clear the discovery failure", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 3 || !reflect.DeepEqual(f.requests[1], f.requests[2]) {
		t.Fatal("endpoint recovery changed the unresolved request")
	}
}
