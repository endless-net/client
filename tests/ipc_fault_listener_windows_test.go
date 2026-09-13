//go:build windows

package tests

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialNativeTestIPC(ctx context.Context, _, pipe string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipe)
}

func listenCLIFaultIPC(_, pipe string) (net.Listener, error) {
	return winio.ListenPipe(pipe, nil)
}
