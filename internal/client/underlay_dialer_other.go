//go:build !linux

package client

import (
	"errors"
	"syscall"
)

func setUnderlaySocketMark(syscall.RawConn, uint32) error {
	return errors.New("marked underlay sockets require Linux")
}

func setUnderlayDNSSocketLink(syscall.RawConn, int) error {
	return errors.New("scoped underlay DNS sockets require Linux")
}
