//go:build linux

package client

import (
	"net"

	"golang.org/x/sys/unix"
)

func setMagicBindSocketMark(socket *net.UDPConn, mark uint32) error {
	if socket == nil {
		return nil
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := raw.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
	}); err != nil {
		return err
	}
	return setErr
}
