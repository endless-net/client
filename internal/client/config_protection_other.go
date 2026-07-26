//go:build !windows

package client

func protectConfigState(raw []byte) ([]byte, error) {
	return raw, nil
}

func unprotectConfigState(raw []byte) ([]byte, error) {
	return raw, nil
}
