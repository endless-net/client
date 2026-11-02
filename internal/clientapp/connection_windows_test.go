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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
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
	store                        *state.Store
	started, bound, revoked      atomic.Bool
	starts, stops, waits, epochs atomic.Uint64
	refreshes, offlines          atomic.Uint64
	wrongPort                    bool
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
	err := connectivity.PersistCursor(ctx, n.store, 1, m)
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
func (n *connectionNativeFixture) WaitDirect(ctx context.Context, _ time.Duration) error {
	value, err := n.store.Load(ctx)
	if err != nil {
		return err
	}
	defer clearConnectionKeys(value)
	p := value.Profiles[workerProfile]
	if !n.started.Load() || p.Connection.EndpointRequest == nil || p.Connection.EndpointGeneration == 1 && p.Connection.EndpointRequest.Receipt == nil {
		return errors.New("path wait before durable endpoint ACK")
	}
	n.waits.Add(1)
	return nil
}
func (n *connectionNativeFixture) ProbeDirect(context.Context) error { return nil }
func (n *connectionNativeFixture) BoundPort() (uint16, error) {
	if !n.bound.Load() {
		return 0, dataplane.ErrClosed
	}
	if n.wrongPort {
		return 51821, nil
	}
	return 51820, nil
}
func (n *connectionNativeFixture) BlockSource() uint64 { return n.epochs.Add(1) }
func (n *connectionNativeFixture) RevokeSource()       { n.revoked.Store(true) }

func (n *connectionNativeFixture) ConnectionLocal() connectivity.LocalObservation {
	return connectivity.LocalObservation{Known: n.started.Load() && !n.revoked.Load(), Applied: n.started.Load()}
}

type endpointClockFunc func() (connectivity.ClockSample, error)

func (f endpointClockFunc) Sample() (connectivity.ClockSample, error) { return f() }

func TestWindowsConnectionRecoveryRefreshesHistoricalEndpointACKBeforeDirect(t *testing.T) {
	for _, mode := range []string{"expired", "changed", "current", "uncertain_next", "expired_next", "budget"} {
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
			f.factory.config = f.config
			endpoint := f.config.Endpoint.String()
			if mode == "changed" {
				endpoint = "192.0.2.99:51821"
			}
			input := coordinator.UpdateNodeEndpointRequest{Endpoint: endpoint, Generation: 1, TTL: f.config.EndpointTTL.String(), Status: "online"}
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
			if mode != "current" && mode != "changed" {
				updated = updated.Add(-120 * time.Second)
			}
			f.acks[1] = coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: endpoint, Generation: 1, Online: true, UpdatedAt: updated, ExpiresAt: updated.Add(f.config.EndpointTTL), Revision: coordinator.MapRevision{Network: 1}}
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
			d := owner.(*windowsConnection)
			defer func() {
				if err := d.Disconnect(workerCallContext(t)); err != nil {
					t.Error(err)
				}
			}()
			err = d.Connect(backendOperation(t, f), false)
			if mode == "budget" {
				if !errors.Is(err, context.DeadlineExceeded) || f.native.waits.Load() != 0 {
					t.Fatal("expired native budget reached next announcement or Direct", err)
				}
			} else if mode == "uncertain_next" || mode == "expired_next" {
				if !errors.Is(err, connectivity.ErrEndpointUncertain) || f.native.waits.Load() != 0 {
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
			want := uint64(2)
			if mode == "current" || mode == "budget" {
				want = 1
			}
			if p.Connection.Generation != 1 || p.Connection.EndpointGeneration != want || !bytes.Equal(f.originals[1], original) || p.Connection.Deadline != value.Profiles[workerProfile].Connection.Deadline {
				t.Fatal("historical receipt used as current lease, frozen request changed or operation renewed", p.Connection.EndpointGeneration, want)
			}
			if mode != "uncertain_next" && mode != "expired_next" && mode != "budget" {
				if receipt := p.Connection.EndpointRequest.Receipt; receipt == nil || receipt.Endpoint != f.config.Endpoint.String() || !receipt.ExpiresAt.After(time.Now()) || f.native.waits.Load() != 1 {
					t.Fatal("Direct admitted without a current matching discovery ACK")
				}
			}
		})
	}
}

type connectionBackendFixture struct {
	store      *state.Store
	native     *connectionNativeFixture
	factory    *windowsConnections
	config     WindowsConnectionConfig
	transport  *connectivity.Transport
	mu         sync.Mutex
	requests   []coordinator.UpdateNodeEndpointRequest
	originals  map[uint64][]byte
	acks       map[uint64]coordinator.UpdateNodeEndpointResponse
	loseSecond bool
	lost       bool
	patches    chan uint64
	streamBody *connectionStreamBody
	afterACK   func(coordinator.UpdateNodeEndpointResponse)
}

func newConnectionBackendFixture(t *testing.T) *connectionBackendFixture {
	t.Helper()
	fixture := &connectionBackendFixture{originals: map[uint64][]byte{}, acks: map[uint64]coordinator.UpdateNodeEndpointResponse{}, patches: make(chan uint64, 32)}
	now := time.Now().UTC()
	bundle := func(seed byte) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32))
		pub := key.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(pub)
		id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
		before, after := now.Add(-time.Hour), now.Add(2*time.Hour)
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
	fixture.store, _ = workerStorePrepared(t, func(p *state.Profile) {
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
		if request.Method == http.MethodPatch {
			if !fixture.native.bound.Load() {
				return nil, errors.New("announcement before socket receipt")
			}
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			input, err := coordinator.DecodeUpdateNodeEndpointRequest(raw, 65536)
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
				updated := time.Now().UTC()
				ttl, _ := time.ParseDuration(input.TTL)
				ack = coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: "node", NetworkID: "network", Endpoint: input.Endpoint, Generation: input.Generation, Online: input.Status == "online", UpdatedAt: updated, ExpiresAt: updated.Add(ttl), Revision: coordinator.MapRevision{Network: revision.Load()}}
				fixture.acks[input.Generation] = ack
			}
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
			body, err := json.Marshal(ack)
			if err != nil {
				return nil, err
			}
			if fixture.afterACK != nil {
				fixture.afterACK(ack)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
		}
		if request.URL.Query().Get("from_network_revision") != "0" || request.URL.Query().Get("from_map_hash") != "" {
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
		issued := time.Now().UTC()
		expires := issued.Add(time.Minute)
		version := revision.Add(1)
		m := coordinator.NetworkMapSnapshot{Revision: coordinator.MapRevision{Network: version}, Network: coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.64.0.0/24", Revision: version}, Node: coordinator.Node{ID: "node", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), AssignedIP: "100.64.0.1", ApprovalState: "approved", Status: "online"}, Peers: []coordinator.Peer{{ID: "peer", NetworkID: "network", PublicKey: base64.StdEncoding.EncodeToString(peer.PublicKey().Bytes()), Endpoint: "192.0.2.2:51820", EndpointGeneration: 1, EndpointExpiresAt: &expires, AllowedIPs: []string{"100.64.0.2/32"}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}}}}
		payload, err := coordinator.NetworkMapSigningPayload(m, mapBundle.ActiveKeyID, issued, expires)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(payload)
		m.MapSignature = &coordinator.MapSignature{Version: 5, Algorithm: "ed25519-network-map-v5", KeyID: mapBundle.ActiveKeyID, IssuedAt: issued, ExpiresAt: expires, PayloadHash: hex.EncodeToString(hash[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(mapKey, payload))}
		event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"}, EventID: "map:" + m.MapSignature.PayloadHash, To: m.Revision, Snapshot: &m, ResultSignature: m.MapSignature}
		body, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		body = append(body, '\n')
		stream := &connectionStreamBody{data: bytes.NewReader(body), closed: make(chan struct{}), ctx: request.Context()}
		fixture.mu.Lock()
		fixture.streamBody = stream
		fixture.mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-ndjson"}, "X-EndlessNet-Map-Protocol": {"3"}, "X-EndlessNet-Map-Capabilities": {"snapshot,signed-delta,checkpoint,resync,heartbeat"}}, Body: stream, Request: request}, nil
	})
	transport, err := connectivity.NewTransport(&http.Client{Transport: trip}, connectivity.HTTPConfig{CallTimeout: time.Second, StreamDuration: 2 * time.Second, ReadIdleTimeout: time.Second, MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 8, MaxCalls: 1, MaxStreams: 1})
	if err != nil {
		t.Fatal(err)
	}
	fixture.transport = transport
	fixture.config = WindowsConnectionConfig{Verifier: connectivity.VerifierConfig{Announcements: connectionAnnouncement{announcement}, Now: time.Now, Clock: workerFixtureClock{}, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192}, Native: dataplane.WindowsSessionConfig{MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, MaxInventoryRows: 256, MaxInterfaces: 256, MTU: 1420, ListenPort: 51820, RouteMetric: 7, DADPollInterval: time.Millisecond, TransactionWait: 100 * time.Millisecond, ApplyTimeout: time.Second}, Endpoint: netip.MustParseAddrPort("192.0.2.1:51820"), EndpointTTL: 200 * time.Millisecond, RenewalInterval: 20 * time.Millisecond, RetryInterval: 20 * time.Millisecond, ProbeInterval: 10 * time.Millisecond, WriteBudget: time.Second, MaxQueuedMaps: 8}
	fixture.factory = &windowsConnections{store: fixture.store, transport: transport, config: fixture.config, stream: transport.StreamSnapshot, cleanup: func(context.Context, string) error { return nil }, makeNative: func(context.Context, string, uint64) (windowsConnectionNative, error) { return fixture.native, nil }}
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

func TestWindowsConnectionDriverUsesSDKMapBoundPortEndpointACKBeforePath(t *testing.T) {
	f := newConnectionBackendFixture(t)
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*windowsConnection)
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
	if p.Connection.MapCursor == nil || p.Connection.EndpointGeneration < 1 || f.native.starts.Load() != 1 || f.native.waits.Load() != 1 {
		t.Fatal("driver skipped actual SDK/state stages")
	}
	if p.Attempt.NodeID != "node" || p.Connection.Generation != 1 {
		t.Fatal("connect changed registration/generation")
	}
}

func TestWindowsConnectionDriverRenewalReplaysLostACKWithoutNewGeneration(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.loseSecond = true
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*windowsConnection)
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
	d := owner.(*windowsConnection)
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
			t.Fatal("source did not reconnect/apply full snapshot")
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

func TestWindowsConnectionDriverRejectsBindMismatchBeforeAnnouncementOrPath(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.native.wrongPort = true
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*windowsConnection)
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
	if len(f.patches) != 0 || f.native.waits.Load() != 0 || value.Profiles[workerProfile].Connection.EndpointGeneration != 0 {
		t.Fatal("unconfirmed bind published or completed path")
	}
}

func TestWindowsConnectionDriverQueueOverflowRevokesWithoutBlocking(t *testing.T) {
	f := newConnectionBackendFixture(t)
	f.factory.config.MaxQueuedMaps = 1
	owner, err := f.factory.newOwner(context.Background(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := owner.(*windowsConnection)
	defer d.cancel()
	if err := d.enqueue(f.native, connectionFrame{}); err != nil {
		t.Fatal(err)
	}
	if err := d.enqueue(f.native, connectionFrame{}); !errors.Is(err, ErrConnectionWorker) || !f.native.revoked.Load() {
		t.Fatal("bounded source queue dropped frame without revoke", err)
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
