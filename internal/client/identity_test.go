package client

import (
	"strings"
	"testing"
)

func TestIdentityKeyPairDerivesPublicKey(t *testing.T) {
	privateKey, err := GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(privateKey, identityPrivateKeyPrefix) {
		t.Fatalf("identity private key = %q, want %s prefix", privateKey, identityPrivateKeyPrefix)
	}
	publicKey, err := IdentityPublicKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(publicKey, identityPublicKeyPrefix) {
		t.Fatalf("identity public key = %q, want %s prefix", publicKey, identityPublicKeyPrefix)
	}
	again, err := IdentityPublicKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if again != publicKey {
		t.Fatalf("identity public key changed: first=%s second=%s", publicKey, again)
	}
	if strings.Contains(privateKey, strings.TrimPrefix(publicKey, identityPublicKeyPrefix)) {
		t.Fatal("identity private key encoding unexpectedly contains the public key encoding")
	}
}

func TestIdentityPublicKeyRejectsMalformedPrivateKey(t *testing.T) {
	for _, value := range []string{"", "private-key", identityPrivateKeyPrefix + "bad"} {
		if _, err := IdentityPublicKey(value); err == nil {
			t.Fatalf("IdentityPublicKey(%q) unexpectedly succeeded", value)
		}
	}
}

func TestSignIdentityIsStableForSamePayload(t *testing.T) {
	privateKey, err := GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	first, err := SignIdentity(privateKey, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignIdentity(privateKey, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("identity signatures = %q/%q, want stable non-empty signature", first, second)
	}
}
