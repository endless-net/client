//go:build windows

package tests

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func listenCLIFaultIPC(_, pipe string) (net.Listener, error) {
	return winio.ListenPipe(pipe, nil)
}
