//go:build !windows

package tests

import (
	"context"
	"net"
)

func dialNativeTestIPC(ctx context.Context, socket, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}

func listenCLIFaultIPC(socket, _ string) (net.Listener, error) {
	return net.Listen("unix", socket)
}
