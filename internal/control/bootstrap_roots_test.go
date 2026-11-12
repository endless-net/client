package control

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestEmbeddedBootstrapRootsBindCanonicalOriginToExpectedPublicRoot(t *testing.T) {
	const origin = "https://api.endlessnet.ru"
	const id = "ed25519:kVpTYm6e_7P3FClgPRZ4Ql83ssXdvlXqC0GVD9FJEC4"
	roots := EmbeddedBootstrapRoots()
	root, ok := roots[origin]
	if !ok || len(roots) != 1 || root.KeyID != id || base64.StdEncoding.EncodeToString(root.PublicKey) != "NUyS36ISrNjTAzf3g+gdJirn4clrb9S/M9qKe+zM7Hs=" {
		t.Fatal("canonical API origin lost its independent release root")
	}
	digest := sha256.Sum256(root.PublicKey)
	if "ed25519:"+base64.RawURLEncoding.EncodeToString(digest[:]) != root.KeyID {
		t.Fatal("embedded root ID differs from its public material")
	}
	for _, alias := range []string{id, "https://endlessnet.ru", "http://api.endlessnet.ru", origin + "/", "https://API.endlessnet.ru", origin + ":443", origin + ".attacker.invalid"} {
		if _, exists := roots[alias]; exists {
			t.Fatal("catalog accepted a noncanonical or foreign origin")
		}
	}
}

func TestEmbeddedRootLookupReachesHTTPSButNeverAcceptsForeignSignature(t *testing.T) {
	const origin = "https://api.endlessnet.ru"
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	// Synthetic signing material stays in the test; it is never inserted into
	// the production root catalog or allowed to advance a checkpoint.
	raw, _ := signedFixture(t, origin, now, 1)
	store, _ := bootstrapStore(t)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := NewVerifiedBootstrap(&http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != origin+"/server-key" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("bootstrap request lost its origin/credential boundary")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
	})}, 1<<20, func() time.Time { return now }, EmbeddedBootstrapRoots(), store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Fetch(t.Context(), origin)
	if calls != 1 || !errors.Is(err, ErrBootstrap) || got.VerifiedAt(now) {
		t.Fatal("origin root missing or foreign signer accepted", calls, err)
	}
	after, err := store.Load(t.Context())
	if err != nil || after.Revision != before.Revision || len(after.BootstrapCheckpoints) != 0 {
		t.Fatal("foreign signer changed protected checkpoint", err)
	}
	if _, err = source.Fetch(t.Context(), origin+".attacker.invalid"); !errors.Is(err, ErrBootstrap) || calls != 1 {
		t.Fatal("unknown origin reached HTTP")
	}
}
