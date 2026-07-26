//go:build darwin

package client

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/sys/unix"
)

func UnixServiceIPCPeerForConn(conn net.Conn) (ServiceIPCPeer, error) {
	if conn == nil {
		return ServiceIPCPeer{}, errors.New("service IPC connection is nil")
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return ServiceIPCPeer{}, fmt.Errorf("service IPC connection %T is not a Unix socket", conn)
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return ServiceIPCPeer{}, fmt.Errorf("service IPC unix socket raw connection: %w", err)
	}
	var (
		cred    *unix.Xucred
		credErr error
	)
	if err := rawConn.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return ServiceIPCPeer{}, fmt.Errorf("service IPC unix socket control: %w", err)
	}
	if credErr != nil {
		return ServiceIPCPeer{}, fmt.Errorf("service IPC unix socket peer credentials: %w", credErr)
	}
	if cred == nil {
		return ServiceIPCPeer{}, errors.New("service IPC unix socket did not provide peer credentials")
	}
	identity := "uid:" + strconv.FormatUint(uint64(cred.Uid), 10)
	return ServiceIPCPeer{
		Transport: ServiceIPCTransportUnixSocket,
		User:      identity,
		Identity:  identity,
		Admin:     cred.Uid == 0,
	}, nil
}
