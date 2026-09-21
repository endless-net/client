//go:build linux

package client

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var errExitLANRouteWatch = errors.New("LAN routing observation invalidated")

// This watches only this process's network namespace routing notifications.
// It is neither WiFi association evidence nor a packet-time instance guard.
// A single notification or any uncertainty permanently revokes the stream.
type exitLANRouteWatch struct {
	file          *os.File
	changed, done chan struct{}
	once          sync.Once
	mu            sync.Mutex
	err, closeErr error
	stopContext   func() bool
}

func openExitLANRouteWatch(ctx context.Context) (exitLANChangeStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, errExitLANRouteWatch
	}
	groups := uint32(0)
	for _, group := range []uint{unix.RTNLGRP_LINK, unix.RTNLGRP_IPV4_IFADDR, unix.RTNLGRP_IPV6_IFADDR, unix.RTNLGRP_IPV4_ROUTE, unix.RTNLGRP_IPV6_ROUTE, unix.RTNLGRP_IPV4_RULE, unix.RTNLGRP_IPV6_RULE} {
		groups |= 1 << (group - 1)
	}
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); err != nil {
		_ = unix.Close(fd)
		return nil, errExitLANRouteWatch
	}
	file := os.NewFile(uintptr(fd), "LAN route notifications")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errExitLANRouteWatch
	}
	raw, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, errExitLANRouteWatch
	}
	w := &exitLANRouteWatch{file: file, changed: make(chan struct{}), done: make(chan struct{})}
	// File.Close wakes RawConn.Read's poller. Recvmsg never blocks in its
	// callback, and RawConn holds the descriptor reference against fd reuse.
	w.stopContext = context.AfterFunc(ctx, func() { w.invalidate(ctx.Err()); <-w.done })
	go func() {
		defer close(w.done)
		defer w.stopContext()
		buffer := make([]byte, 64<<10)
		var n, flags int
		var from unix.Sockaddr
		var receiveErr error
		err := raw.Read(func(fd uintptr) bool {
			for {
				select {
				case <-w.changed:
					receiveErr = context.Canceled
					return true
				default:
				}
				n, _, flags, from, receiveErr = unix.Recvmsg(int(fd), buffer, nil, unix.MSG_DONTWAIT)
				if receiveErr == unix.EINTR {
					continue
				}
				return receiveErr != unix.EAGAIN && receiveErr != unix.EWOULDBLOCK
			}
		})
		if err != nil || receiveErr != nil || n <= 0 || n > len(buffer) {
			w.invalidate(errExitLANRouteWatch)
			return
		}
		w.invalidate(exitLANRouteDatagram(buffer[:n], flags, from))
	}()
	if err := ctx.Err(); err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

func (w *exitLANRouteWatch) Changed() <-chan struct{} { return w.changed }
func (w *exitLANRouteWatch) Err() error               { w.mu.Lock(); defer w.mu.Unlock(); return w.err }
func (w *exitLANRouteWatch) invalidate(err error) {
	w.once.Do(func() {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.changed)
		closeErr := w.file.Close()
		w.mu.Lock()
		w.closeErr = closeErr
		w.mu.Unlock()
	})
}
func (w *exitLANRouteWatch) Close() error {
	w.stopContext()
	w.invalidate(context.Canceled)
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeErr
}

// No datagram is ignored: even unexpected control messages revoke the stream.
// Parsing only decides whether revocation is a known topology change or loss /
// malformed evidence. Sender identity comes from recvmsg, never header PID alone.
func exitLANRouteDatagram(raw []byte, flags int, from unix.Sockaddr) error {
	sender, ok := from.(*unix.SockaddrNetlink)
	if !ok || sender.Family != unix.AF_NETLINK || sender.Pid != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || len(raw) == 0 || len(raw) > 64<<10 {
		return errExitLANRouteWatch
	}
	for len(raw) > 0 {
		if len(raw) < unix.NLMSG_HDRLEN {
			return errExitLANRouteWatch
		}
		size := int(binary.NativeEndian.Uint32(raw[:4]))
		kind := binary.NativeEndian.Uint16(raw[4:6])
		if size < unix.NLMSG_HDRLEN || size > len(raw) || binary.NativeEndian.Uint32(raw[12:16]) != 0 {
			return errExitLANRouteWatch
		}
		minimum := 0
		switch kind {
		case unix.RTM_NEWLINK, unix.RTM_DELLINK:
			minimum = unix.SizeofIfInfomsg
		case unix.RTM_NEWADDR, unix.RTM_DELADDR:
			minimum = unix.SizeofIfAddrmsg
		case unix.RTM_NEWROUTE, unix.RTM_DELROUTE, unix.RTM_NEWRULE, unix.RTM_DELRULE:
			minimum = unix.SizeofRtMsg
		default:
			return errExitLANRouteWatch
		}
		if size < unix.NLMSG_HDRLEN+minimum {
			return errExitLANRouteWatch
		}
		attributes := raw[unix.NLMSG_HDRLEN+minimum : size]
		for len(attributes) > 0 {
			if len(attributes) < 4 {
				return errExitLANRouteWatch
			}
			length := int(binary.NativeEndian.Uint16(attributes[:2]))
			if length < 4 || length > len(attributes) {
				return errExitLANRouteWatch
			}
			step := (length + 3) &^ 3
			if step > len(attributes) {
				if length != len(attributes) {
					return errExitLANRouteWatch
				}
				break
			}
			attributes = attributes[step:]
		}
		aligned := (size + 3) &^ 3
		if aligned > len(raw) {
			if size != len(raw) {
				return errExitLANRouteWatch
			}
			return nil
		}
		raw = raw[aligned:]
	}
	return nil
}
