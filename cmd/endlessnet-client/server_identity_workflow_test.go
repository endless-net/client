package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestTypedServerIdentityInspectionDoesNotAdoptAnnouncement(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted := testSigningTrustBundle(t, base64.RawURLEncoding.EncodeToString(public))
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	announced := testSigningTrustBundle(t, base64.RawURLEncoding.EncodeToString(other))
	for _, invalid := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("public identity lookup leaked credentials")
			}
			bundle := *announced
			if invalid {
				bundle = clientapi.SigningTrustBundle{}
			}
			_ = json.NewEncoder(w).Encode(testServerKeyResponseFromBundle(bundle))
		}))
		cfg := client.Config{ControlPlaneURLs: []string{server.URL}, MapSigningTrust: trusted, Token: "synthetic-session", NodeCredential: "synthetic-node"}
		local, remote, err := inspectConfiguredServerIdentity(t.Context(), cfg)
		server.Close()
		if (err != nil) != invalid {
			t.Fatal("invalid announcement validation", err)
		}
		if !invalid && (local.ActiveKeyID != trusted.ActiveKeyID || remote.ActiveKeyID != announced.ActiveKeyID) {
			t.Fatal("trust identities lost")
		}
		if cfg.MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID || cfg.Token != "synthetic-session" {
			t.Fatal("inspection mutated local authority")
		}
	}
}

func TestTypedServerIdentityInspectionCancellation(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted := testSigningTrustBundle(t, base64.RawURLEncoding.EncodeToString(public))
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := inspectConfiguredServerIdentity(ctx, client.Config{ControlPlaneURLs: []string{server.URL}, MapSigningTrust: trusted})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("identity request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("request cancellation lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("identity request ignored cancellation")
	}
}
