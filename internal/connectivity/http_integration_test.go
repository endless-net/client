//go:build integration

package connectivity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
	signingwire "github.com/endless-net/signing/api/v1"
	"github.com/endless-net/signing/api/v1/signingapiconnect"
)

func TestPublicTLSMapTransportReceivesBeforeEOFAndThenSignedRevoke(t *testing.T) {
	if testing.Short() {
		t.Skip("separate owner public TLS streaming fixture")
	}
	f := newMapFixture(t)
	firstIssued := f.now.Add(1200 * time.Millisecond)
	first := f.sign(t, nil, firstIssued, f.now.Add(time.Minute))
	cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: firstIssued}
	issued := firstIssued.Add(time.Second)
	f.value.Node.Status = "revoked"
	f.value.Peers = []coordinator.Peer{}
	f.value.Revision.Network++
	f.value.Network.Revision = f.value.Revision.Network
	last := append(f.sign(t, &cursor, issued, issued.Add(time.Minute)), '\n')
	firstReceived := make(chan struct{})
	var keyCredentialLeak atomic.Bool
	var mapCalls atomic.Int32
	var bootstrapResponse atomic.Pointer[signingwire.GetBootstrapResponse]
	var bootstrapUnavailable atomic.Bool
	mux := http.NewServeMux()
	_, bootstrapHandler := signingapiconnect.NewBootstrapServiceHandler(bootstrapOwner{get: func(r *connect.Request[signingwire.GetBootstrapRequest]) (*connect.Response[signingwire.GetBootstrapResponse], error) {
		if r.Header().Get("X-EndlessNet-Node-Credential") != "" || r.Header().Get("Authorization") != "" || r.Header().Get("Cookie") != "" {
			keyCredentialLeak.Store(true)
		}
		if bootstrapUnavailable.Load() {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("fixture bootstrap unavailable"))
		}
		return connect.NewResponse(bootstrapResponse.Load()), nil
	}})
	mux.Handle(signingapiconnect.BootstrapServiceGetBootstrapProcedure, bootstrapHandler)
	_, mapsHandler := publicqueryv1connect.NewConnectivityServiceHandler(mapOwner{watch: func(ctx context.Context, r *connect.Request[wire.WatchMapsRequest], stream *connect.ServerStream[wire.WatchMapsResponse]) error {
		if r.Msg.NodeId != "node" || r.Header().Get("X-EndlessNet-Node-Credential") != f.profile.Secrets.NodeCredential {
			return connect.NewError(connect.CodePermissionDenied, errors.New("denied"))
		}
		mapCalls.Add(1)
		var opening, final coordinator.MapStreamEvent
		if err := json.Unmarshal(f.openingRecord(t, first, r.Msg.Nonce), &opening); err != nil {
			return err
		}
		if err := json.Unmarshal(last, &final); err != nil {
			return err
		}
		if err := stream.Send(testwire.Update(opening)); err != nil {
			return err
		}
		select {
		case <-firstReceived:
			return stream.Send(testwire.Update(final))
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	mux.Handle(publicqueryv1connect.ConnectivityServiceWatchMapsProcedure, mapsHandler)
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	announcement, err := control.DecodeAnnouncement(server.URL, f.trust.Bundle, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.trust = announcement
	f.profile.ControlOrigin = server.URL
	f.profile.Attempt.ControlOrigin = server.URL
	f.profile.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{73}, 32))
	public := private.Public().(ed25519.PublicKey)
	rootDigest := sha256.Sum256(public)
	root := signing.BootstrapRoot{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(rootDigest[:]), PublicKey: public}
	var bundles signing.ServerKeyResponse
	if err := json.Unmarshal(f.trust.Bundle, &bundles); err != nil {
		t.Fatal(err)
	}
	metadata := signing.BootstrapMetadata{Origin: server.URL, Revision: 1, IssuedAt: f.now.Add(-time.Second), ExpiresAt: f.now.Add(time.Minute), Bundles: bundles}
	signedBootstrap := func(metadata signing.BootstrapMetadata) *signingwire.GetBootstrapResponse {
		t.Helper()
		message, err := signing.BootstrapSigningBytes(metadata, root.KeyID)
		if err != nil {
			t.Fatal(err)
		}
		response, err := signing.NewBootstrapResponse(metadata, root.KeyID, ed25519.Sign(private, message))
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	original := signedBootstrap(metadata)
	bootstrapResponse.Store(original)
	protected, _ := cursorStore(t, f)
	bootstrap, err := control.NewVerifiedBootstrap(server.Client(), 65536, f.config.Now, map[string]signing.BootstrapRoot{server.URL: root}, protected)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Announcements = bootstrap
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewTransport(server.Client(), HTTPConfig{CallTimeout: time.Second, StreamDuration: 2 * time.Second, ReadIdleTimeout: time.Second, MaxRequestBytes: 65536, MaxResponseBytes: 65536, MaxRecords: 4, MaxCalls: 1, MaxStreams: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bootstrapUnavailable.Store(true)
	if err := transport.Stream(ctx, v, nil, func(context.Context, Map) error {
		t.Error("verified map arrived during bootstrap outage")
		return nil
	}); !errors.Is(err, ErrUnavailable) || mapCalls.Load() != 0 {
		t.Fatalf("bootstrap outage did not remain a retryable control failure: %v", err)
	}
	bootstrapUnavailable.Store(false)
	received := 0
	err = transport.Stream(ctx, v, nil, func(_ context.Context, m Map) error {
		received++
		if received == 1 {
			if m.Denied() || !f.now.Before(firstIssued) {
				t.Error("first map not delivered while stream open")
			}
			f.now = issued
			close(firstReceived)
		} else if received != 2 || !m.Denied() {
			t.Error("signed revoke lost")
		}
		return nil
	})
	if !errors.Is(err, ErrRevoked) || received != 2 || mapCalls.Load() != 1 || keyCredentialLeak.Load() {
		t.Fatal("public TLS stream/credential separation failed", err, received)
	}

	// Advance to a distinct map key under the pinned bootstrap root. A fresh
	// map signed by that key can be accepted while a rollback still fails closed.
	f.now = issued.Add(time.Second)
	f.mapPrivate = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{74}, ed25519.SeedSize))
	mapPublic := f.mapPrivate.Public().(ed25519.PublicKey)
	mapDigest := sha256.Sum256(mapPublic)
	mapKey := &metadata.Bundles.TrustBundle.Keys[0]
	mapKey.KeyID = "ed25519:" + base64.RawURLEncoding.EncodeToString(mapDigest[:])
	mapKey.PublicKey = base64.RawURLEncoding.EncodeToString(mapPublic)
	metadata.Bundles.TrustBundle.ActiveKeyID = mapKey.KeyID
	metadata.Revision++
	rotated := signedBootstrap(metadata)
	bootstrapResponse.Store(rotated)
	raw, err := json.Marshal(metadata.Bundles)
	if err != nil {
		t.Fatal(err)
	}
	f.trust, err = control.DecodeAnnouncement(server.URL, raw, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.value.Node.Status = "online"
	f.value.Revision.Network++
	f.value.Network.Revision = f.value.Revision.Network
	fresh := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	if got, err := v.VerifySnapshot(ctx, fresh, nil); err != nil || got.Denied() || got.ExpiresAt().IsZero() {
		t.Fatalf("authorized map-key rotation did not accept the fresh map: %v", err)
	}
	bootstrapResponse.Store(original)
	if got, err := v.VerifySnapshot(ctx, fresh, nil); !errors.Is(err, ErrTrust) || !got.ExpiresAt().IsZero() {
		t.Fatalf("rollback after rotation did not fail closed: %v", err)
	}
}

type bootstrapOwner struct {
	signingapiconnect.UnimplementedBootstrapServiceHandler
	get func(*connect.Request[signingwire.GetBootstrapRequest]) (*connect.Response[signingwire.GetBootstrapResponse], error)
}

func (o bootstrapOwner) GetBootstrap(_ context.Context, r *connect.Request[signingwire.GetBootstrapRequest]) (*connect.Response[signingwire.GetBootstrapResponse], error) {
	return o.get(r)
}

type mapOwner struct {
	publicqueryv1connect.UnimplementedConnectivityServiceHandler
	watch func(context.Context, *connect.Request[wire.WatchMapsRequest], *connect.ServerStream[wire.WatchMapsResponse]) error
}

func (o mapOwner) WatchMaps(ctx context.Context, r *connect.Request[wire.WatchMapsRequest], stream *connect.ServerStream[wire.WatchMapsResponse]) error {
	return o.watch(ctx, r, stream)
}
