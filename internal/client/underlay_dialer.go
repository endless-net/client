package client

import (
	"context"
	"net"
	"syscall"
)

// Mark both the transport and its resolver sockets before connecting. Marking
// only TCP leaves relay hostname resolution subject to the exit route/guard.
func markedUnderlayDialer(mark uint32, setMark func(syscall.RawConn, uint32) error) *net.Dialer {
	dialer := &net.Dialer{}
	if mark == 0 {
		return dialer
	}
	if setMark == nil {
		setMark = setUnderlaySocketMark
	}
	control := func(_, _ string, raw syscall.RawConn) error { return setMark(raw, mark) }
	dialer.Control = control
	dialer.Resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			resolverDialer := net.Dialer{Control: control}
			return resolverDialer.DialContext(ctx, network, address)
		},
	}
	return dialer
}
