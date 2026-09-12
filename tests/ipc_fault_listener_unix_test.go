//go:build !windows

package tests

import "net"

func listenCLIFaultIPC(socket, _ string) (net.Listener, error) {
	return net.Listen("unix", socket)
}
