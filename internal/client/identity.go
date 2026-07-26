package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	identityPrivateKeyPrefix = "eni_"
	identityPublicKeyPrefix  = "enp_"
)

func GenerateIdentityPrivateKey() (string, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	return identityPrivateKeyPrefix + base64.RawURLEncoding.EncodeToString(privateKey), nil
}

func IdentityPublicKey(privateKey string) (string, error) {
	raw, err := decodeIdentityPrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	publicKey, ok := ed25519.PrivateKey(raw).Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("invalid identity public key")
	}
	return identityPublicKeyPrefix + base64.RawURLEncoding.EncodeToString(publicKey), nil
}

func SignIdentity(privateKey string, payload []byte) (string, error) {
	raw, err := decodeIdentityPrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(ed25519.PrivateKey(raw), payload)
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeIdentityPrivateKey(privateKey string) ([]byte, error) {
	privateKey = strings.TrimSpace(privateKey)
	if !strings.HasPrefix(privateKey, identityPrivateKeyPrefix) {
		return nil, errors.New("invalid identity private key")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(privateKey, identityPrivateKeyPrefix))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid identity private key")
	}
	return raw, nil
}
