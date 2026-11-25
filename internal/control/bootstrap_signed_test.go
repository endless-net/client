package control

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/signing/api/v1/signingapiconnect"

	"github.com/endless-net/client/internal/state"
	signing "github.com/endless-net/signing/api/contract"
	"google.golang.org/protobuf/proto"
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

func TestBootstrapRenewalDistinguishesOutageFromRejectedTrust(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	good, root := signedFixture(t, "https://control.example", now, 1)
	for _, mode := range []string{"transport", "503", "429", "tls", "redirect", "malformed", "401"} {
		t.Run(mode, func(t *testing.T) {
			store, _ := bootstrapStore(t)
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if mode == "transport" {
					return nil, io.ErrUnexpectedEOF
				}
				if mode == "tls" {
					return nil, x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"foreign.example"}}, Host: "control.example"}
				}
				status, body := 200, good
				if mode == "503" {
					status = 503
				}
				if mode == "429" {
					status = 429
				}
				if mode == "401" {
					status = 401
				}
				if mode == "malformed" {
					body = []byte(`{"invalid":true}`)
				}
				header := http.Header{"Content-Type": {"application/json"}}
				if mode == "redirect" {
					status = 302
					header.Set("Location", "https://foreign.example"+signingapiconnect.BootstrapServiceGetBootstrapProcedure)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
			})}
			source, err := NewVerifiedBootstrap(client, 1<<20, func() time.Time { return now }, map[string]signing.BootstrapRoot{"https://control.example": root}, store)
			if err != nil {
				t.Fatal(err)
			}
			value, err := source.Fetch(t.Context(), "https://control.example")
			transient := mode == "transport" || mode == "503" || mode == "429"
			if !errors.Is(err, ErrBootstrap) || errors.Is(err, ErrBootstrapUnavailable) != transient || value.VerifiedAt(now) {
				t.Fatal("outage/trust boundary changed", mode, err)
			}
		})
	}
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
		return bootstrapReply(r, *raw), nil
	})}
	source, err := NewVerifiedBootstrap(client, 1<<20, func() time.Time { return *now }, map[string]signing.BootstrapRoot{"https://control.example": root}, store)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestConnectionBootstrapSharesOnlyCurrentVerifiedAnnouncement(t *testing.T) {
	const origin = "https://control.example"
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	raw, root := signedFixture(t, origin, now, 11)
	store, _ := bootstrapStore(t)
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		return bootstrapReply(r, raw), nil
	})}
	source, err := NewVerifiedBootstrap(client, 1<<20, func() time.Time { return now }, map[string]signing.BootstrapRoot{origin: root}, store)
	if err != nil {
		t.Fatal(err)
	}
	startup, finish := context.WithCancel(t.Context())
	defer finish()
	scoped := source.ForConnection(startup, origin)
	for range 3 {
		first, err := scoped.Fetch(t.Context(), origin)
		if err != nil || !first.VerifiedAt(now) || first.Checkpoint().Revision != 11 {
			t.Fatal("valid bootstrap was rejected", err)
		}
		first.Bundle[0] ^= 0xff
	}
	if calls != 1 {
		t.Fatal("expected one GET for startup", calls)
	}
	if _, err := scoped.Fetch(t.Context(), "https://other.example"); !errors.Is(err, ErrBootstrap) || calls != 1 {
		t.Fatal("startup bootstrap crossed origins", err)
	}
	raw, _ = signedFixture(t, origin, now, 12)
	if _, err := source.Fetch(t.Context(), origin); err != nil {
		t.Fatal(err)
	}
	if current, err := scoped.Fetch(t.Context(), origin); err != nil || current.Checkpoint().Revision != 12 || calls != 3 {
		t.Fatal("startup reused a superseded checkpoint", err, calls)
	}
	now = now.Add(time.Hour)
	if _, err := scoped.Fetch(t.Context(), origin); !errors.Is(err, ErrBootstrap) || calls != 4 {
		t.Fatal("startup accepted expired bootstrap", err, calls)
	}
	raw, _ = signedFixture(t, origin, now, 13)
	finish()
	for range 2 {
		if current, err := scoped.Fetch(t.Context(), origin); err != nil || current.Checkpoint().Revision != 13 {
			t.Fatal("background fetch was rejected", err)
		}
	}
	if calls != 6 {
		t.Fatal("bootstrap outlived connection scope", calls)
	}
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
	if result, err := source.Fetch(t.Context(), "https://control.example"); !errors.Is(err, ErrBootstrapState) || !errors.Is(err, state.ErrUncertain) || errors.Is(err, ErrBootstrap) || result.ID != "" {
		t.Fatal("lost commit ACK was misclassified or yielded a usable bundle", err)
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

func bootstrapReply(r *http.Request, raw []byte) *http.Response {
	if signed, err := signing.DecodeSignedBootstrap(raw); err == nil {
		signature, _ := base64.RawURLEncoding.DecodeString(signed.Signature)
		if message, err := signing.NewBootstrapResponse(signed.Metadata, signed.RootKeyID, signature); err == nil {
			if encoded, err := proto.Marshal(message); err == nil {
				raw = encoded
			}
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/proto"}},
		Body: io.NopCloser(bytes.NewReader(raw)), Request: r}
}

func TestBootstrapSeparatesPermanentUpstreamErrorsFromOutages(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeFailedPrecondition, connect.CodeDataLoss, connect.CodeUnimplemented} {
		if got := bootstrapResponseError(connect.NewError(code, errors.New("fixture"))); !errors.Is(got, ErrBootstrapProtocol) {
			t.Errorf("%v became %v", code, got)
		}
	}
}
