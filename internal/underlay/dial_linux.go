// Package underlay configures sockets that carry the tunnel or its control traffic.
package underlay

import (
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Mark identifies the service's outer packets to Linux policy routing.
// Socket marks and the firewall's per-session conntrack mark are independent.
const Mark = 0x454e4554

func Control(_, _ string, raw syscall.RawConn) error {
	return markSocket(raw, unix.SetsockoptInt)
}

func markSocket(raw syscall.RawConn, set func(int, int, int, int) error) error {
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		socketErr = set(int(fd), unix.SOL_SOCKET, unix.SO_MARK, Mark)
	}); err != nil {
		return err
	}
	return socketErr
}

func Dialer(timeout time.Duration) *net.Dialer {
	// The resolver uses the system nameserver configuration. Its own upstream
	// socket needs the same routing mark even before the remote IP is known.
	dns := &net.Dialer{Timeout: timeout, Control: Control}
	return &net.Dialer{Timeout: timeout, Control: Control, Resolver: &net.Resolver{PreferGo: true, Dial: dns.DialContext}}
}
