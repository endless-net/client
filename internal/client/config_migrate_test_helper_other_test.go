//go:build !windows

package client

func protectLegacyConfigStateForTest(raw []byte) ([]byte, error) {
	return raw, nil
}
