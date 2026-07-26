package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const legacyWindowsConfigProtectionFormat = "endlessnet-client-state-v1"

func unprotectLegacyConfigState(raw []byte) ([]byte, error) {
	var envelope windowsProtectedConfigState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode protected legacy client state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("protected legacy client state contains trailing JSON")
		}
		return nil, fmt.Errorf("decode protected legacy client state: %w", err)
	}
	if envelope.Format != legacyWindowsConfigProtectionFormat {
		return nil, fmt.Errorf("unsupported protected legacy state format %q", envelope.Format)
	}
	if envelope.Protection != windowsConfigProtectionProvider {
		return nil, fmt.Errorf("unsupported protected legacy state provider %q", envelope.Protection)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode protected legacy state: %w", err)
	}
	return cryptUnprotectMachine(ciphertext)
}
