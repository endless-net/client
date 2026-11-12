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

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

func TestPublicTLSMapTransportReceivesBeforeEOFAndThenSignedRevoke(t *testing.T) {
	if testing.Short() {
		t.Skip("separate owner public TLS streaming fixture")
	}
	f := newMapFixture(t)
	first := append(f.sign(t, nil, f.now, f.now.Add(time.Minute)), '\n')
	cursor := state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
	issued := f.now.Add(time.Second)
	f.value.Node.Status = "revoked"
	f.value.Peers = []coordinator.Peer{}
	f.value.Revision.Network++
	f.value.Network.Revision = f.value.Revision.Network
	last := append(f.sign(t, &cursor, issued, issued.Add(time.Minute)), '\n')
	firstReceived := make(chan struct{})
	var keyCredentialLeak atomic.Bool
	var bootstrapRaw []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server-key" {
			if r.Header.Get("X-EndlessNet-Node-Credential") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				keyCredentialLeak.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(bootstrapRaw)
			return
		}
		if r.URL.Path != "/maps/node/stream" || r.Header.Get("X-EndlessNet-Node-Credential") != f.profile.Secrets.NodeCredential {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-EndlessNet-Map-Protocol", "3")
		w.Header().Set("X-EndlessNet-Map-Capabilities", "snapshot,signed-delta,checkpoint,resync,heartbeat")
		_, _ = w.Write(first)
		if http.NewResponseController(w).Flush() != nil {
			return
		}
		select {
		case <-firstReceived:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(last)
		_ = http.NewResponseController(w).Flush()
	}))
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
	message, err := signing.BootstrapSigningBytes(metadata, root.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapRaw, err = json.Marshal(signing.SignedBootstrap{Version: 2, Purpose: "bootstrap", RootKeyID: root.KeyID, Metadata: metadata, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, message))})
	if err != nil {
		t.Fatal(err)
	}
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
	received := 0
	err = transport.Stream(ctx, v, nil, func(_ context.Context, m Map) error {
		received++
		if received == 1 {
			if m.Denied() {
				t.Error("first map not delivered while stream open")
			}
			f.now = issued
			close(firstReceived)
		} else if received != 2 || !m.Denied() {
			t.Error("signed revoke lost")
		}
		return nil
	})
	if !errors.Is(err, ErrRevoked) || received != 2 || keyCredentialLeak.Load() {
		t.Fatal("public TLS stream/credential separation failed", err, received)
	}
}
