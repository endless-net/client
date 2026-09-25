//go:build !linux

package testclient

import "errors"

func newTestLogindBus() (string, func(), error) {
	return "", nil, errors.New("isolated test logind is available only on Linux")
}
