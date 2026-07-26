//go:build !windows

package client

import (
	"context"
	"errors"
	"net"
)

const WindowsServicePipeSDDL = `D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)`

func ListenWindowsServicePipe(name string) (net.Listener, error) {
	return nil, errors.New("windows service pipe is only supported on Windows")
}

func WindowsServiceIPCConnContext(ctx context.Context, conn net.Conn) context.Context {
	return ctx
}
