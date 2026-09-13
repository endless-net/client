//go:build windows

package tests

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func dialNativeTestIPC(ctx context.Context, _, pipe string) (net.Conn, error) {
	// Match the production SDK's caller-identification capability. Only the
	// contract-header injector is bypassed by the negotiation probes.
	return winio.DialPipeAccessImpLevel(ctx, pipe,
		windows.GENERIC_READ|windows.GENERIC_WRITE, winio.PipeImpLevelImpersonation)
}

func listenCLIFaultIPC(_, pipe string) (net.Listener, error) {
	return winio.ListenPipe(pipe, nil)
}
