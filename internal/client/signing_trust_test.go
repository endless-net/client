package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestLoadSigningTrustFileAndRejectReplacement(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyText := base64.RawURLEncoding.EncodeToString(publicKey)
	bundle, err := clientapi.NewSigningTrustBundle(publicKeyText)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "map-signing-trust.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSigningTrustFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{}
	if err := SetSigningTrustBundle(&cfg, loaded); err != nil {
		t.Fatal(err)
	}
	if cfg.MapSigningTrust == nil || cfg.MapSigningTrust.ActiveKeyID != bundle.ActiveKeyID {
		t.Fatalf("configured signing trust = %#v", cfg)
	}

	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(otherPublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := SetSigningTrustBundle(&cfg, other); err == nil {
		t.Fatal("unauthenticated trust-anchor replacement succeeded")
	}
	if err := ReplaceSigningTrustBundle(&cfg, other); err != nil {
		t.Fatalf("explicit trust-anchor replacement failed: %v", err)
	}
}

func TestSigningTrustBundleStagedRotationAndRollbackProtection(t *testing.T) {
	oldPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(oldPublic))
	if err != nil {
		t.Fatal(err)
	}
	newBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(newPublic))
	if err != nil {
		t.Fatal(err)
	}
	overlapOldActive := clientapi.SigningTrustBundle{
		Version:     clientapi.SigningTrustBundleVersion,
		ActiveKeyID: oldBundle.ActiveKeyID,
		Keys:        []clientapi.SigningTrustKey{oldBundle.Keys[0], newBundle.Keys[0]},
	}
	overlapNewActive := overlapOldActive
	overlapNewActive.ActiveKeyID = newBundle.ActiveKeyID

	addCandidate := Config{}
	if err := ReplaceSigningTrustBundle(&addCandidate, oldBundle); err != nil {
		t.Fatal(err)
	}
	if err := SetSigningTrustBundle(&addCandidate, overlapNewActive); err == nil {
		t.Fatal("network response added a signing key that was not pretrusted")
	}

	cfg := Config{}
	if err := ReplaceSigningTrustBundle(&cfg, overlapOldActive); err != nil {
		t.Fatal(err)
	}
	if err := SetSigningTrustBundle(&cfg, overlapNewActive); err != nil {
		t.Fatalf("pretrusted active-key switch failed: %v", err)
	}
	if cfg.MapSigningTrust == nil || cfg.MapSigningTrust.ActiveKeyID != newBundle.ActiveKeyID || len(cfg.MapSigningTrust.Keys) != 2 {
		t.Fatalf("active-key switch was not persisted: %#v", cfg.MapSigningTrust)
	}
	if err := SetSigningTrustBundle(&cfg, newBundle); err != nil {
		t.Fatalf("retired-key removal failed: %v", err)
	}
	if cfg.MapSigningTrust == nil || cfg.MapSigningTrust.ActiveKeyID != newBundle.ActiveKeyID || len(cfg.MapSigningTrust.Keys) != 1 {
		t.Fatalf("retired-key removal was not persisted: %#v", cfg.MapSigningTrust)
	}
	if err := SetSigningTrustBundle(&cfg, oldBundle); err == nil {
		t.Fatal("retired signing key rollback was accepted")
	}
}

func TestServerTrustUpdatePreservesFutureActivationWindow(t *testing.T) {
	oldPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(oldPublic))
	if err != nil {
		t.Fatal(err)
	}
	newBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(newPublic))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	notBefore := now.Add(time.Hour)
	futureKey := newBundle.Keys[0]
	futureKey.NotBefore = &notBefore
	local := clientapi.SigningTrustBundle{
		Version:     clientapi.SigningTrustBundleVersion,
		ActiveKeyID: oldBundle.ActiveKeyID,
		Keys:        []clientapi.SigningTrustKey{oldBundle.Keys[0], futureKey},
	}
	cfg := Config{}
	if err := ReplaceSigningTrustBundle(&cfg, local); err != nil {
		t.Fatal(err)
	}

	announced := clientapi.SigningTrustBundle{
		Version:     clientapi.SigningTrustBundleVersion,
		ActiveKeyID: oldBundle.ActiveKeyID,
		Keys:        []clientapi.SigningTrustKey{oldBundle.Keys[0], newBundle.Keys[0]},
	}
	if err := setSigningTrustBundleFromServerAt(&cfg, announced, now); err != nil {
		t.Fatalf("staging an inactive future key failed: %v", err)
	}
	if got := cfg.MapSigningTrust.Keys[1].NotBefore; got == nil || !got.Equal(notBefore) {
		t.Fatalf("future NotBefore = %v, want %v", got, notBefore)
	}

	announced.ActiveKeyID = newBundle.ActiveKeyID
	if err := setSigningTrustBundleFromServerAt(&cfg, announced, now); err == nil {
		t.Fatal("server activated a locally trusted key before NotBefore")
	}
	if err := setSigningTrustBundleFromServerAt(&cfg, announced, notBefore); err != nil {
		t.Fatalf("future key activation at NotBefore failed: %v", err)
	}
	if cfg.MapSigningTrust.ActiveKeyID != newBundle.ActiveKeyID {
		t.Fatalf("activated trust = %#v", cfg.MapSigningTrust)
	}
}

func TestLoadSigningTrustFileRejectsSinglePublicKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "map-signing-public-key.txt")
	if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(publicKey)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigningTrustFile(path); err == nil {
		t.Fatal("single public key was accepted as a trust bundle")
	}
}

func TestServerTrustUpdateCannotExtendLocalValidity(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	notBefore := now.Add(-time.Hour)
	notAfter := now.Add(time.Hour)
	localKey := bundle.Keys[0]
	localKey.NotBefore = &notBefore
	localKey.NotAfter = &notAfter
	local := bundle
	local.Keys = []clientapi.SigningTrustKey{localKey}
	cfg := Config{}
	if err := ReplaceSigningTrustBundle(&cfg, local); err != nil {
		t.Fatal(err)
	}

	// The server omits both bounds and supplies different provider metadata.
	announced := bundle
	announced.Keys[0].Provider = "attacker-controlled"
	announced.Keys[0].ProviderVersion = 999
	if err := setSigningTrustBundleFromServerAt(&cfg, announced, now); err != nil {
		t.Fatal(err)
	}
	persisted := cfg.MapSigningTrust.Keys[0]
	if persisted.NotBefore == nil || !persisted.NotBefore.Equal(notBefore) || persisted.NotAfter == nil || !persisted.NotAfter.Equal(notAfter) {
		t.Fatalf("server changed local validity bounds: %#v", persisted)
	}
	if persisted.Provider == announced.Keys[0].Provider || persisted.ProviderVersion == announced.Keys[0].ProviderVersion {
		t.Fatalf("server changed local trust metadata: %#v", persisted)
	}
}

func TestServerTrustUpdateRetiresToLocalSubsetRecords(t *testing.T) {
	oldPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(oldPublic))
	if err != nil {
		t.Fatal(err)
	}
	newBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(newPublic))
	if err != nil {
		t.Fatal(err)
	}
	newLocalKey := newBundle.Keys[0]
	newLocalKey.Provider = "operator-pinned"
	local := clientapi.SigningTrustBundle{
		Version:     clientapi.SigningTrustBundleVersion,
		ActiveKeyID: oldBundle.ActiveKeyID,
		Keys:        []clientapi.SigningTrustKey{oldBundle.Keys[0], newLocalKey},
	}
	cfg := Config{}
	if err := ReplaceSigningTrustBundle(&cfg, local); err != nil {
		t.Fatal(err)
	}
	announced := newBundle
	announced.Keys[0].Provider = "server-value"
	if err := SetSigningTrustBundle(&cfg, announced); err != nil {
		t.Fatal(err)
	}
	if cfg.MapSigningTrust == nil || len(cfg.MapSigningTrust.Keys) != 1 || cfg.MapSigningTrust.Keys[0].KeyID != newBundle.ActiveKeyID {
		t.Fatalf("retired trust subset = %#v", cfg.MapSigningTrust)
	}
	if cfg.MapSigningTrust.Keys[0].Provider != "operator-pinned" {
		t.Fatalf("retired subset did not preserve local record: %#v", cfg.MapSigningTrust.Keys[0])
	}
}
