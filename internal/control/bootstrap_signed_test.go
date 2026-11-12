package control

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	signing "github.com/endless-net/signing/api/contract"
)

func bootstrapStore(t *testing.T) (*state.Store, *memory) {
	t.Helper()
	block, err := aes.NewCipher(bytes.Repeat([]byte{74}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	backend := &memory{}
	store, err := state.New(backend, testCodec{aead}, "753c9536-feb4-43a5-8d55-7a3c1b7e215d", 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	return store, backend
}

func signedFixture(t *testing.T, origin string, now time.Time, revision uint64) ([]byte, signing.BootstrapRoot) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{83}, 32))
	public := private.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	root := signing.BootstrapRoot{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:]), PublicKey: public}
	metadata := signing.BootstrapMetadata{Origin: origin, Revision: revision, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Bundles: fixture(t)}
	start, end := now.Add(-time.Hour), now.Add(2*time.Hour)
	for _, bundle := range []*signing.BootstrapTrustBundle{&metadata.Bundles.TrustBundle, &metadata.Bundles.NodeCredentialTrustBundle, &metadata.Bundles.RelayTrustBundle} {
		for i := range bundle.Keys {
			bundle.Keys[i].NotBefore, bundle.Keys[i].NotAfter = &start, &end
		}
	}
	message, err := signing.BootstrapSigningBytes(metadata, root.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	raw := encode(t, signing.SignedBootstrap{Version: 2, Purpose: "bootstrap", RootKeyID: root.KeyID, Metadata: metadata, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, message))})
	return raw, root
}

func signedSource(t *testing.T, raw *[]byte, now *time.Time, root signing.BootstrapRoot, store *state.Store) *Bootstrap {
	t.Helper()
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(*raw)), Request: r}, nil
	})}
	source, err := NewVerifiedBootstrap(client, 1<<20, func() time.Time { return *now }, map[string]signing.BootstrapRoot{"https://control.example": root}, store)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSignedBootstrapRootTimeOriginAndSignatureRefusals(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	good, root := signedFixture(t, "https://control.example", now, 7)
	for _, name := range []string{"unsigned", "signature", "root-id", "root-material", "origin", "expired", "future", "unknown-root", "missing-root"} {
		t.Run(name, func(t *testing.T) {
			clock := now
			raw := bytes.Clone(good)
			pinned := root
			var envelope signing.SignedBootstrap
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "unsigned":
				raw = encode(t, envelope.Metadata.Bundles)
			case "signature":
				envelope.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
				raw = encode(t, envelope)
			case "root-id":
				envelope.RootKeyID = "ed25519:" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
				raw = encode(t, envelope)
			case "root-material":
				pinned.PublicKey = bytes.Repeat([]byte{99}, 32)
			case "origin":
				raw, _ = signedFixture(t, "https://other.example", now, 7)
			case "expired":
				clock = now.Add(time.Hour)
			case "future":
				clock = now.Add(-2 * time.Minute)
			case "unknown-root":
				pinned.KeyID = "unknown"
			}
			store, _ := bootstrapStore(t)
			source := signedSource(t, &raw, &clock, pinned, store)
			if name == "missing-root" {
				source.roots = nil
			}
			result, err := source.Fetch(t.Context(), "https://control.example")
			if !errors.Is(err, ErrBootstrap) || result.ID != "" {
				t.Fatal("unverified response escaped", err)
			}
			saved, err := store.Load(t.Context())
			if err != nil || len(saved.BootstrapCheckpoints) != 0 {
				t.Fatal("refusal changed high-water", err)
			}
		})
	}
}

func TestSignedBootstrapDurableReplayRollbackEquivocationAndLostACK(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	raw, root := signedFixture(t, "https://control.example", now, 7)
	store, backend := bootstrapStore(t)
	source := signedSource(t, &raw, &now, root, store)
	first, err := source.Fetch(t.Context(), "https://control.example")
	if err != nil || !first.VerifiedAt(now) {
		t.Fatal(err)
	}
	saved, err := store.Load(t.Context())
	if err != nil || saved.BootstrapCheckpoints[first.Origin] != first.Checkpoint() {
		t.Fatal("returned before durable high-water", err)
	}
	plain, err := DecodeAnnouncement(first.Origin, first.Bundle, now)
	if err != nil || plain.VerifiedAt(now) {
		t.Fatal("shape decode became trust proof", err)
	}
	first.Bundle[0] = '!'
	if first.VerifiedAt(now) {
		t.Fatal("mutated proof accepted")
	}
	// Reconstruct source using the same durable state: restart does not reset floor.
	source = signedSource(t, &raw, &now, root, store)
	if _, err := source.Fetch(t.Context(), "https://control.example"); err != nil {
		t.Fatal("same signed revision rejected", err)
	}
	raw, _ = signedFixture(t, "https://control.example", now, 6)
	if _, err := source.Fetch(t.Context(), "https://control.example"); !errors.Is(err, ErrBootstrap) {
		t.Fatal("rollback accepted")
	}
	raw, _ = signedFixture(t, "https://control.example", now.Add(time.Second), 7)
	if _, err := source.Fetch(t.Context(), "https://control.example"); !errors.Is(err, ErrBootstrap) {
		t.Fatal("equivocation accepted")
	}
	raw, _ = signedFixture(t, "https://control.example", now, 8)
	backend.lose = true
	if result, err := source.Fetch(t.Context(), "https://control.example"); !errors.Is(err, ErrBootstrap) || result.ID != "" {
		t.Fatal("lost commit ACK yielded usable bundle", err)
	}
	if _, err := source.Fetch(t.Context(), "https://control.example"); err != nil {
		t.Fatal("fresh verify/read did not resolve lost ACK", err)
	}
	raw, _ = signedFixture(t, "https://control.example", now, 7)
	if _, err := source.Fetch(t.Context(), "https://control.example"); !errors.Is(err, ErrBootstrap) {
		t.Fatal("lost ACK reset high-water")
	}
}

func TestOrdinaryBootstrapCannotFetchUnsignedOrLearnRoot(t *testing.T) {
	calls := 0
	source, err := NewBootstrap(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected network") })}, 65536, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Fetch(context.Background(), "https://control.example"); !errors.Is(err, ErrBootstrap) || calls != 0 {
		t.Fatal("no-root fallback attempted", err)
	}
}

func TestVerifiedRevisionRefreshDoesNotRequireOldProfilePinMatch(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	raw, root := signedFixture(t, "https://control.example", now, 1)
	store, _ := bootstrapStore(t)
	source := signedSource(t, &raw, &now, root, store)
	old, err := source.Fetch(t.Context(), "https://control.example")
	if err != nil {
		t.Fatal(err)
	}
	profile := state.Profile{ControlOrigin: old.Origin, Trust: state.ServerTrust{KeyID: old.KeyID, AnnouncementID: old.ID, Bundle: old.Bundle}}
	raw, _ = signedFixture(t, old.Origin, now.Add(time.Second), 2)
	current, err := source.Fetch(t.Context(), old.Origin)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID == old.ID || !current.MatchesProfile(profile, now) {
		t.Fatal("verified refresh locked to old profile pin")
	}
	decoded, err := DecodeAnnouncement(current.Origin, current.Bundle, now)
	if err != nil || decoded.MatchesProfile(profile, now) {
		t.Fatal("shape decode approved changed pin", err)
	}
	profile.ControlOrigin = "https://other.example"
	if current.MatchesProfile(profile, now) {
		t.Fatal("verified origin crossed profile boundary")
	}
	if current.VerifiedAt(now.Add(2*time.Hour)) || current.VerifiedAt(now.Add(-2*time.Minute)) {
		t.Fatal("retained verification ignored metadata time")
	}
}
