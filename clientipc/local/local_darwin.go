//go:build darwin

package local

import (
	"errors"
	"net"
	"strconv"

	"golang.org/x/sys/unix"
)

func identify(conn net.Conn) (Peer, error) {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return Peer{}, errors.New("connection is not a Unix socket")
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var cred *unix.Xucred
	var credErr error
	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	})
	if err != nil {
		return Peer{}, err
	}
	if credErr != nil {
		return Peer{}, credErr
	}
	if cred == nil {
		return Peer{}, errors.New("socket has no peer credentials")
	}
	return Peer{Identity: "uid:" + strconv.FormatUint(uint64(cred.Uid), 10), Administrator: cred.Uid == 0}, nil
}
