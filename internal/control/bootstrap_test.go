package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	signingcontract "github.com/endless-net/signing/api/contract"
)

func fixture(t *testing.T) signingcontract.ServerKeyResponse {
	t.Helper()
	bundle := func(b byte) signingcontract.BootstrapTrustBundle {
		raw := bytes.Repeat([]byte{b}, 32)
		digest := sha256.Sum256(raw)
		key := signingcontract.BootstrapTrustKey{KeyID: "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:]), Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(raw)}
		return signingcontract.BootstrapTrustBundle{Version: 1, ActiveKeyID: key.KeyID, Keys: []signingcontract.BootstrapTrustKey{key}}
	}
	return signingcontract.ServerKeyResponse{TrustBundle: bundle(1), NodeCredentialTrustBundle: bundle(2), RelayTrustBundle: bundle(3)}
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBootstrapSDKAndCompleteAnnouncementBinding(t *testing.T) {
	now := time.Now().UTC()
	value := fixture(t)
	raw := encode(t, value)
	first, err := DecodeAnnouncement("https://control.example", raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Validate(now) != nil {
		t.Fatal("canonical producer projection not stable")
	}
	other, err := DecodeAnnouncement("https://other.example", raw, now)
	if err != nil || first.ID == other.ID {
		t.Fatal("announcement not origin-bound", err)
	}
	value.NodeCredentialTrustBundle.Keys[0].ProviderVersion = 2
	changed, err := DecodeAnnouncement("https://control.example", encode(t, value), now)
	if err != nil || changed.KeyID != first.KeyID || changed.ID == first.ID {
		t.Fatal("same map key hid node-purpose change", err)
	}
	value = fixture(t)
	value.NodeCredentialTrustBundle = value.TrustBundle
	if _, err = DecodeAnnouncement(first.Origin, encode(t, value), now); !errors.Is(err, ErrBootstrap) {
		t.Fatal("shared purpose material accepted")
	}
	value = fixture(t)
	value.NodeCredentialTrustBundle.Keys[0].NotAfter = &now
	if _, err = DecodeAnnouncement(first.Origin, encode(t, value), now); !errors.Is(err, ErrBootstrap) {
		t.Fatal("expired active purpose accepted")
	}
	if _, err = DecodeAnnouncement(first.Origin, append(raw, raw...), now); !errors.Is(err, ErrBootstrap) {
		t.Fatal("extra JSON accepted")
	}
	if _, err = DecodeAnnouncement(first.Origin, []byte(`{"unknown":true}`), now); !errors.Is(err, ErrBootstrap) {
		t.Fatal("unknown fields accepted")
	}
}

func TestBootstrapBoundedPublicFetchAndNoRedirect(t *testing.T) {
	now := time.Now().UTC()
	raw, root := signedFixture(t, "https://control.example", now, 1)
	store, _ := bootstrapStore(t)
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://control.example/server-key" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("bootstrap changed surface or sent user credentials")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("unbounded bootstrap")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
	})}
	b, err := NewVerifiedBootstrap(client, 1<<20, func() time.Time { return now }, map[string]signingcontract.BootstrapRoot{"https://control.example": root}, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Fetch(context.Background(), "https://control.example"); err != nil || calls != 1 {
		t.Fatal(err)
	}
	b.maxBytes = 10
	if _, err = b.Fetch(context.Background(), "https://control.example"); !errors.Is(err, ErrBootstrap) {
		t.Fatal("oversized bootstrap accepted")
	}
	client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": []string{"https://other.example/server-key"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	b, err = NewVerifiedBootstrap(client, 1<<20, nil, map[string]signingcontract.BootstrapRoot{"https://control.example": root}, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Fetch(context.Background(), "https://control.example"); !errors.Is(err, ErrBootstrap) {
		t.Fatal("bootstrap redirect followed")
	}
	for _, cfg := range []*tls.Config{{InsecureSkipVerify: true}, {Certificates: []tls.Certificate{{}}}} {
		if _, err = NewBootstrap(&http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}, 1<<20, nil); !errors.Is(err, ErrBootstrap) {
			t.Fatal("unsafe bootstrap TLS client accepted")
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewBootstrap(&http.Client{Jar: jar}, 1<<20, nil); !errors.Is(err, ErrBootstrap) {
		t.Fatal("ambient browser session accepted")
	}
}
