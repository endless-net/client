//go:build !windows

package client

func unprotectLegacyConfigState(raw []byte) ([]byte, error) {
	return raw, nil
}
