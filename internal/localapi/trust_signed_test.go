package localapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localipc"
	signing "github.com/endless-net/signing/api/contract"
)

type bootstrapTransport func(*http.Request) (*http.Response, error)

func (f bootstrapTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRootVerifiedWakePinsAutomaticallyAndAcceptsNextRevision(t *testing.T) {
	c, _, runner, now := fixture(t)
	origin := c.config.DefaultControlOrigin
	shape, err := c.config.Announcements.Fetch(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	var bundles signing.ServerKeyResponse
	if err := json.Unmarshal(shape.Bundle, &bundles); err != nil {
		t.Fatal(err)
	}
	start, end := now.Add(-time.Hour), now.Add(2*time.Hour)
	for _, bundle := range []*signing.BootstrapTrustBundle{&bundles.TrustBundle, &bundles.NodeCredentialTrustBundle, &bundles.RelayTrustBundle} {
		for i := range bundle.Keys {
			bundle.Keys[i].NotBefore, bundle.Keys[i].NotAfter = &start, &end
		}
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{91}, 32))
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	root := signing.BootstrapRoot{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:]), PublicKey: public}
	var raw []byte
	sign := func(revision uint64) {
		metadata := signing.BootstrapMetadata{Origin: origin, Revision: revision, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Bundles: bundles}
		message, err := signing.BootstrapSigningBytes(metadata, root.KeyID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = json.Marshal(signing.SignedBootstrap{Version: 2, Purpose: "bootstrap", RootKeyID: root.KeyID, Metadata: metadata, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, message))})
		if err != nil {
			t.Fatal(err)
		}
	}
	sign(1)
	source, err := control.NewVerifiedBootstrap(&http.Client{Transport: bootstrapTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
	})}, 1<<20, c.config.Now, map[string]signing.BootstrapRoot{origin: root}, c.store)
	if err != nil {
		t.Fatal(err)
	}
	c.config.Announcements = source
	result, err := c.begin(t.Context(), localipc.Principal{SID: "S-1-5-21-11"}, request(1))
	if err != nil || runner.calls != 1 || result.Attempt.Restriction != nil {
		t.Fatal("verified initial trust required manual acceptance", err)
	}
	saved, err := c.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := saved.Profiles[defaultID]
	if original.Trust.KeyID == "" || saved.BootstrapCheckpoints[origin].Revision != 1 {
		t.Fatal("wake preceded protected pin")
	}
	// A legitimate working-key validity change alters the full announcement ID.
	later := end.Add(time.Hour)
	bundles.RelayTrustBundle.Keys[0].NotAfter = &later
	sign(2)
	if err := c.wake(t.Context(), defaultID, original.Attempt.AttemptID); err != nil || runner.calls != 2 {
		t.Fatal("verified new revision permanently locked on old pin", err)
	}
	saved, err = c.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current := saved.Profiles[defaultID]
	if current.Trust.AnnouncementID == original.Trust.AnnouncementID || !bytes.Equal(current.Keys.IdentityPrivate, original.Keys.IdentityPrivate) || current.Attempt.Binding != original.Attempt.Binding {
		t.Fatal("rotation failed to preserve attempt/key identity")
	}
	sign(1)
	if err := c.wake(t.Context(), defaultID, original.Attempt.AttemptID); err == nil || runner.calls != 2 {
		t.Fatal("rolled-back bootstrap woke worker")
	}
}
