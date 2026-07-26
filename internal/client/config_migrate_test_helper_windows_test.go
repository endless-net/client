package client

import (
	"encoding/base64"
	"encoding/json"
)

func protectLegacyConfigStateForTest(raw []byte) ([]byte, error) {
	protected, err := cryptProtectMachine(raw)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(windowsProtectedConfigState{
		Format:     legacyWindowsConfigProtectionFormat,
		Protection: windowsConfigProtectionProvider,
		Ciphertext: base64.StdEncoding.EncodeToString(protected),
	}, "", "  ")
}
