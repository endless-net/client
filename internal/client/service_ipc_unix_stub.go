//go:build !linux && !darwin

package client

import (
	"context"
	"errors"
	"net"

	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"
)

func DefaultLocalServiceSocketPath() string {
	return ipc.DefaultUnixSocket
}

func ListenUnixServiceSocket(path string) (net.Listener, error) {
	return nil, errors.New("unix service socket is only supported on Linux and macOS")
}

func UnixServiceIPCConnContext(ctx context.Context, conn net.Conn) context.Context {
	return ctx
}
