//go:build !linux

package client

import "errors"

func newPlatformExitGuard(string) (*linuxExitGuard, error) {
	return nil, errors.New("protected exit startup is unavailable on this platform")
}
