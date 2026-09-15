//go:build linux

package client

import "net"

func setMagicBindSocketMark(socket *net.UDPConn, mark uint32) error {
	if socket == nil {
		return nil
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	return setUnderlaySocketMark(raw, mark)
}
