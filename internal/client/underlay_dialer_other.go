//go:build !linux

package client

import (
	"errors"
	"syscall"
)

func setUnderlaySocketMark(syscall.RawConn, uint32) error {
	return errors.New("marked underlay sockets require Linux")
}
