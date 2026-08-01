package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func HasSigningTrust(cfg Config) bool {
	return cfg.MapSigningTrust != nil
}

func SigningTrustBundle(cfg Config) (clientapi.SigningTrustBundle, error) {
	if cfg.MapSigningTrust == nil {
		return clientapi.SigningTrustBundle{}, errors.New("map signing trust anchor is required")
	}
	bundle := cloneSigningTrustBundle(*cfg.MapSigningTrust)
	if err := bundle.Validate(); err != nil {
		return clientapi.SigningTrustBundle{}, err
	}
	return bundle, nil
}

func SetSigningTrustBundle(cfg *Config, bundle clientapi.SigningTrustBundle) error {
	return setSigningTrustBundleFromServerAt(cfg, bundle, time.Now().UTC())
}

func setSigningTrustBundleFromServerAt(cfg *Config, bundle clientapi.SigningTrustBundle, at time.Time) error {
	if cfg == nil {
		return errors.New("client config is required")
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if HasSigningTrust(*cfg) {
		existing, err := SigningTrustBundle(*cfg)
		if err != nil {
			return err
		}
		localKeys := make(map[string]clientapi.SigningTrustKey, len(existing.Keys))
		for _, key := range existing.Keys {
			localKeys[key.KeyID] = cloneSigningTrustKey(key)
		}
		accepted := clientapi.SigningTrustBundle{
			Version:     bundle.Version,
			ActiveKeyID: bundle.ActiveKeyID,
			Keys:        make([]clientapi.SigningTrustKey, 0, len(bundle.Keys)),
		}
		for _, key := range bundle.Keys {
			trusted, ok := localKeys[key.KeyID]
			if !ok || trusted.PublicKey != key.PublicKey || trusted.Algorithm != key.Algorithm {
				return errors.New("map signing trust anchor replacement requires an authenticated rotation")
			}
			// The unsigned server bundle may select an active key and retire
			// locally trusted keys, but it cannot rewrite any local key metadata
			// (especially NotBefore/NotAfter validity bounds).
			accepted.Keys = append(accepted.Keys, trusted)
		}
		bundle = accepted
		if err := bundle.Validate(); err != nil {
			return err
		}
	}
	if _, err := bundle.Resolve(bundle.ActiveKeyID, at); err != nil {
		return err
	}
	return setSigningTrustBundle(cfg, bundle)
}

// ReplaceSigningTrustBundle is reserved for an explicit operator-supplied
// trust key or trust file. Network responses must use SetSigningTrustBundle.
func ReplaceSigningTrustBundle(cfg *Config, bundle clientapi.SigningTrustBundle) error {
	if cfg == nil {
		return errors.New("client config is required")
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if _, err := bundle.Resolve(bundle.ActiveKeyID, time.Now().UTC()); err != nil {
		return err
	}
	return setSigningTrustBundle(cfg, bundle)
}

func setSigningTrustBundle(cfg *Config, bundle clientapi.SigningTrustBundle) error {
	copy := cloneSigningTrustBundle(bundle)
	cfg.MapSigningTrust = &copy
	return nil
}

func cloneSigningTrustBundle(bundle clientapi.SigningTrustBundle) clientapi.SigningTrustBundle {
	copy := bundle
	copy.Keys = make([]clientapi.SigningTrustKey, len(bundle.Keys))
	for index, key := range bundle.Keys {
		copy.Keys[index] = cloneSigningTrustKey(key)
	}
	return copy
}

func cloneSigningTrustKey(key clientapi.SigningTrustKey) clientapi.SigningTrustKey {
	copy := key
	if key.NotBefore != nil {
		value := key.NotBefore.UTC()
		copy.NotBefore = &value
	}
	if key.NotAfter != nil {
		value := key.NotAfter.UTC()
		copy.NotAfter = &value
	}
	return copy
}

func LoadSigningTrustFile(path string) (clientapi.SigningTrustBundle, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return clientapi.SigningTrustBundle{}, errors.New("map signing trust file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return clientapi.SigningTrustBundle{}, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return clientapi.SigningTrustBundle{}, errors.New("map signing trust file is empty")
	}
	var bundle clientapi.SigningTrustBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return clientapi.SigningTrustBundle{}, fmt.Errorf("decode map signing trust file: %w", err)
	}
	if err := bundle.Validate(); err != nil {
		return clientapi.SigningTrustBundle{}, err
	}
	return bundle, nil
}
