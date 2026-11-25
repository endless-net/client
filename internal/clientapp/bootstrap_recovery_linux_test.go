//go:build linux && integration

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
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
	signingwire "github.com/endless-net/signing/api/v1"
	"github.com/endless-net/signing/api/v1/signingapiconnect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const fixtureTunnelPort = 51820

func TestLinuxBootstrapOutageRestoresNativeTunnel(t *testing.T) {
	if testing.Short() || os.Geteuid() != 0 {
		t.Skip("requires root, iproute2 and nftables in an isolated network namespace")
	}
	const marker = "EN_TEST_BOOTSTRAP_NETWORK_NAMESPACE"
	if os.Getenv(marker) != t.Name() {
		ctx, stop := context.WithTimeout(t.Context(), 40*time.Second)
		defer stop()
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(ctx, "unshare", "--net", "--", binary, "-test.run=^"+t.Name()+"$")
		child.Env = append(os.Environ(), marker+"="+t.Name())
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("isolated Linux recovery: %v\n%s", err, output)
		}
		return
	}
	if output, err := exec.CommandContext(t.Context(), "ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Fatalf("enable namespace loopback: %v: %s", err, output)
	}
	now := time.Now().UTC()
	network, networkKey := recoveryFixtureBundle(61, now)
	node, nodeKey := recoveryFixtureBundle(62, now)
	relay, _ := recoveryFixtureBundle(63, now)
	root, rootKey := recoveryFixtureBundle(64, now)
	_, foreignKey := recoveryFixtureBundle(65, now)
	bundles := signing.ServerKeyResponse{TrustBundle: network, NodeCredentialTrustBundle: node, RelayTrustBundle: relay}
	fixture := &recoveryGatewayFixture{mapKey: networkKey, mapKeyID: network.ActiveKeyID, streams: make(chan chan struct{}, 8)}
	mux := http.NewServeMux()
	mux.Handle(signingapiconnect.NewBootstrapServiceHandler(fixture))
	mux.Handle(publicqueryv1connect.NewConnectivityServiceHandler(fixture))
	gateway := httptest.NewUnstartedServer(mux)
	gateway.EnableHTTP2 = true
	gateway.StartTLS()
	t.Cleanup(gateway.Close)
	raw, err := json.Marshal(bundles)
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := control.DecodeAnnouncement(gateway.URL, raw, now)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := workerStorePrepared(t, func(p *state.Profile) {
		p.ControlOrigin, p.Attempt.ControlOrigin = gateway.URL, gateway.URL
		p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
		p.Secrets.NodeCredential = recoveryFixtureCredential(t, p, node.ActiveKeyID, nodeKey)
		key, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			t.Fatal(err)
		}
		fixture.nodePublic = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	})
	metadata := signing.BootstrapMetadata{Origin: gateway.URL, Revision: 1, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Bundles: bundles}
	fixture.bootstrap = recoveryFixtureBootstrap(t, metadata, root.ActiveKeyID, rootKey)
	metadata.Revision++
	fixture.foreign = recoveryFixtureBootstrap(t, metadata, root.ActiveKeyID, foreignKey)
	clock := workerFixtureClock{}
	if err := workerIntent(t, store, state.IntentConnected, now, workerRuntime); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := control.NewVerifiedBootstrap(gateway.Client(), 65536, time.Now, map[string]signing.BootstrapRoot{
		gateway.URL: {KeyID: root.ActiveKeyID, PublicKey: rootKey.Public().(ed25519.PublicKey)},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := connectivity.NewTransport(gateway.Client(), connectivity.HTTPConfig{
		CallTimeout: time.Second, StreamDuration: 15 * time.Second, ReadIdleTimeout: 10 * time.Second,
		MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 8, MaxCalls: 2, MaxStreams: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.CloseIdleConnections)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(directory)
	resources, err := dataplane.NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	factory := connectionFactory{
		store: store, transport: transport, stream: transport.StreamSnapshot,
		cleanup: func(ctx context.Context, _ string) error { return dataplane.CleanLinuxNetwork(ctx, ledger) },
		config: connectionConfig{
			Verifier: connectivity.VerifierConfig{Announcements: bootstrap, Clock: clock, Now: time.Now, CallTimeout: time.Second, MaxBytes: 65536, MaxPeers: 8, MaxRules: 16, MaxCredentialBytes: 8192},
			Endpoint: netip.MustParseAddrPort("192.0.2.1:51820"), EndpointTTL: time.Minute,
			RenewalInterval: 20 * time.Second, RetryInterval: 80 * time.Millisecond, ProbeInterval: 100 * time.Millisecond,
			WriteBudget: time.Second, ApplyTimeout: 2 * time.Second, ListenPort: fixtureTunnelPort, MaxQueuedMaps: 8,
			ReportFailure: func(_ uint64, stage string, err error) { t.Logf("%s: %v", stage, err) },
		},
		makeNative: func(ctx context.Context, profile string, generation uint64) (nativeConnectionSession, error) {
			return dataplane.NewLinuxSession(ctx, store, ledger, clock, profile, generation, dataplane.LinuxSessionConfig{
				MTU: 1420, MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, ListenPort: fixtureTunnelPort,
				ProbeInterval: time.Second, RetryInterval: time.Second,
				Relay: dataplane.RelayConfig{DialTimeout: time.Second, IOTimeout: 3 * time.Second, HeartbeatInterval: time.Second, Now: time.Now},
			}, resources)
		},
	}
	owner, err := factory.newOwner(t.Context(), workerProfile, 1)
	if err != nil {
		t.Fatal(err)
	}
	driver := owner.(*connectionDriver)
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := driver.Disconnect(ctx); err != nil {
			t.Error(err)
		}
	})
	sample, err := clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	budget, err := connectivity.NewOperationBudget(clock, workerProfile, 1, sample, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if err := driver.Connect(connectivity.WithOperationBudget(ctx, budget), false); err != nil {
		t.Fatalf("initial Linux Connect: %v", err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}

	for _, long := range []bool{false, true} {
		initial := waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return local.Known && local.Applied && local.Permission.NewFlows
		})
		if !initial.Applied || !initial.Permission.NewFlows {
			t.Fatalf("Linux tunnel never became ready: %#v", initial)
		}
		fixture.mode.Store(http.StatusServiceUnavailable)
		close(recoveryFixtureStream(t, fixture.streams))
		offline := waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return fixture.unavailable.Load() > 0 && local.Known && local.ControlUnavailable && !local.Permission.NewFlows && local.Permission.ExistingFlows
		})
		if !offline.Permission.ExistingFlows || !offline.Permission.ExpiresAt.Equal(initial.Permission.ExpiresAt) {
			t.Fatalf("outage changed the original lease: %#v", offline)
		}
		if long {
			waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
				return !time.Now().Before(initial.Permission.ExpiresAt) && local.Known && local.ControlUnavailable && !local.Permission.NewFlows && !local.Permission.ExistingFlows
			})
		}
		fixture.mode.Store(http.StatusOK)
		waitRecoveryObservation(t, driver, func(local connectivity.LocalObservation) bool {
			return local.Applied && !local.ControlUnavailable && local.Permission.NewFlows && local.MapRevision > initial.MapRevision
		})
		fixture.unavailable.Store(0)
	}

	fixture.mode.Store(http.StatusForbidden)
	close(recoveryFixtureStream(t, fixture.streams))
	select {
	case <-driver.done:
		if err := driver.result(); !errors.Is(err, connectivity.ErrTrust) || driver.ConnectionLocal().Permission.ExistingFlows {
			t.Fatalf("foreign signing identity did not fail closed: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("foreign signing identity did not terminate the connection")
	}
}

func recoveryFixtureStream(t *testing.T, streams <-chan chan struct{}) chan struct{} {
	t.Helper()
	select {
	case stream := <-streams:
		return stream
	case <-time.After(time.Second):
		t.Fatal("no established map stream to interrupt")
		return nil
	}
}

func waitRecoveryObservation(t *testing.T, driver *connectionDriver, ready func(connectivity.LocalObservation) bool) connectivity.LocalObservation {
	t.Helper()
	timeout := time.NewTimer(7 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		local := driver.ConnectionLocal()
		if ready(local) {
			return local
		}
		select {
		case <-driver.done:
			t.Fatalf("Linux tunnel stopped instead of recovering: %v", driver.result())
		case <-timeout.C:
			t.Fatalf("Linux recovery did not reach the expected state: %#v", local)
		case <-tick.C:
		}
	}
}

type recoveryGatewayFixture struct {
	signingapiconnect.UnimplementedBootstrapServiceHandler
	publicqueryv1connect.UnimplementedConnectivityServiceHandler

	mapKey               ed25519.PrivateKey
	mapKeyID, nodePublic string
	bootstrap, foreign   *signingwire.GetBootstrapResponse
	streams              chan chan struct{}
	mode                 atomic.Int32
	unavailable          atomic.Uint64
	revision             atomic.Uint64
}

func (f *recoveryGatewayFixture) GetBootstrap(_ context.Context, _ *connect.Request[signingwire.GetBootstrapRequest]) (*connect.Response[signingwire.GetBootstrapResponse], error) {
	switch f.mode.Load() {
	case http.StatusServiceUnavailable:
		f.unavailable.Add(1)
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fixture bootstrap unavailable"))
	case http.StatusForbidden:
		return connect.NewResponse(f.foreign), nil
	default:
		return connect.NewResponse(f.bootstrap), nil
	}
}

func (f *recoveryGatewayFixture) UpdateNodeEndpoint(_ context.Context, req *connect.Request[wire.UpdateNodeEndpointRequest]) (*connect.Response[wire.UpdateNodeEndpointResponse], error) {
	now := time.Now().UTC()
	input := req.Msg
	return connect.NewResponse(&wire.UpdateNodeEndpointResponse{
		NodeId: "node", NetworkId: "network", Endpoint: input.Endpoint, Generation: input.Generation,
		Online: input.Status == "online", Transport: input.Transport,
		UpdatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(input.Ttl.AsDuration())),
		Revision: &wire.MapRevision{Network: f.revision.Load()},
	}), nil
}

func (f *recoveryGatewayFixture) WatchMaps(ctx context.Context, req *connect.Request[wire.WatchMapsRequest], stream *connect.ServerStream[wire.WatchMapsResponse]) error {
	now := time.Now().UTC()
	revision := f.revision.Add(1)
	value := coordinator.NetworkMapSnapshot{
		Revision: coordinator.MapRevision{Network: revision},
		Network:  coordinator.Network{ID: "network", AccountID: "account", CIDR: "100.64.0.0/24", Revision: revision},
		Node:     coordinator.Node{ID: "node", NetworkID: "network", PublicKey: f.nodePublic, AssignedIP: "100.64.0.1", ApprovalState: "approved", Status: "online"},
		Peers:    []coordinator.Peer{},
	}
	expiry := now.Add(3 * time.Second)
	payload, err := coordinator.NetworkMapSigningPayload(value, f.mapKeyID, now, expiry)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	signature := &coordinator.MapSignature{Version: 7, Algorithm: "ed25519-network-map-v7", KeyID: f.mapKeyID, IssuedAt: now, ExpiresAt: expiry,
		PayloadHash: hex.EncodeToString(digest[:]), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapKey, payload))}
	value.MapSignature = signature
	proof := &coordinator.MapTimeProof{Nonce: req.Msg.Nonce, MapHash: signature.PayloadHash, ServerTime: now}
	message, err := coordinator.MapTimeProofSigningPayload(*proof, *signature)
	if err != nil {
		return err
	}
	proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.mapKey, message))
	event := coordinator.MapStreamEvent{Type: "snapshot", ProtocolVersion: 3, Capabilities: []string{"snapshot", "signed-delta", "checkpoint", "resync", "heartbeat"},
		EventID: "map:" + signature.PayloadHash, To: value.Revision, Snapshot: &value, ResultSignature: signature, TimeProof: proof}
	if err := stream.Send(testwire.Update(event)); err != nil {
		return err
	}
	cut := make(chan struct{})
	select {
	case f.streams <- cut:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-cut:
		return connect.NewError(connect.CodeUnavailable, errors.New("fixture stream interrupted"))
	case <-ctx.Done():
		return ctx.Err()
	}
}

func recoveryFixtureBundle(seed byte, now time.Time) (signing.BootstrapTrustBundle, ed25519.PrivateKey) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	id := "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:])
	before, after := now.Add(-time.Hour), now.Add(2*time.Hour)
	return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{
		{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public), NotBefore: &before, NotAfter: &after},
	}}, private
}

func recoveryFixtureBootstrap(t *testing.T, metadata signing.BootstrapMetadata, keyID string, key ed25519.PrivateKey) *signingwire.GetBootstrapResponse {
	t.Helper()
	raw, err := signing.BootstrapSigningBytes(metadata, keyID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := signing.NewBootstrapResponse(metadata, keyID, ed25519.Sign(key, raw))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func recoveryFixtureCredential(t *testing.T, profile *state.Profile, keyID string, key ed25519.PrivateKey) string {
	t.Helper()
	p := profile.Attempt
	scopes := []string{"node:register", "node:map", "node:endpoint"}
	raw, err := coordinator.NodeCredentialSigningPayload(keyID, p.NetworkID, p.NodeID, scopes, p.CredentialEnds)
	if err != nil {
		t.Fatal(err)
	}
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: keyID, NetworkID: p.NetworkID, NodeID: p.NodeID, Scopes: scopes,
		ExpiresAt: p.CredentialEnds, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw))}
	raw, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "enc_" + base64.RawURLEncoding.EncodeToString(raw)
}
