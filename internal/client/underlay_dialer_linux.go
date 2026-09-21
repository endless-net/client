//go:build linux

package client

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func setUnderlaySocketMark(raw syscall.RawConn, mark uint32) error {
	var setErr error
	if err := raw.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
	}); err != nil {
		return err
	}
	return setErr
}

func setUnderlayDNSSocketLink(raw syscall.RawConn, index int) error {
	var setErr error
	if err := raw.Control(func(fd uintptr) { setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BINDTOIFINDEX, index) }); err != nil {
		return err
	}
	return setErr
}
