//go:build linux

package client

import (
	"context"
	"encoding/binary"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type exitLANHookTransport interface {
	Port() uint32
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, bool, bool, error)
	Close() error
}

// This is a point-in-time dump in the caller's current network namespace. It
// does not pin objects, establish namespace ownership, or authorize LAN traffic.
func newNativeExitLANHookObservation(ctx context.Context, identity exitLANBPFLinkIdentity) error {
	var order binary.ByteOrder = binary.LittleEndian
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		order = binary.BigEndian
	}
	return observeExitLANHookWith(ctx, identity, order, openNativeExitLANHookTransport)
}

func observeExitLANHookWith(ctx context.Context, identity exitLANBPFLinkIdentity, order binary.ByteOrder, open func(context.Context) (exitLANHookTransport, error)) (result error) {
	return observeExitLANHookStateWith(ctx, identity, order, open, true)
}

func observeExitLANHookStateWith(ctx context.Context, identity exitLANBPFLinkIdentity, order binary.ByteOrder, open func(context.Context) (exitLANHookTransport, error), present bool) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if open == nil {
		return errExitLANBPF
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	transport, err := open(ctx)
	if err != nil {
		if transport != nil {
			_ = transport.Close()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errExitLANBPF
	}
	if transport == nil {
		return errExitLANBPF
	}
	defer func() {
		if err := transport.Close(); err != nil && result == nil {
			result = errExitLANBPF
		}
		if ctx.Err() != nil {
			result = ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	dump, err := newExitLANHookDump(order, 1, transport.Port(), identity)
	if err != nil {
		return errExitLANBPF
	}
	if err := transport.Send(ctx, dump.request()); err != nil {
		return errExitLANBPF
	}
	// A hostile or constantly changing namespace cannot keep this collector
	// alive indefinitely, even if recvmsg never needs to wait on the poller.
	for messages := 0; messages < 128; messages++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, kernel, truncated, err := transport.Receive(ctx)
		if err != nil {
			return errExitLANBPF
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(raw) == 0 || len(raw) > 64<<10 {
			return errExitLANBPF
		}
		if err := dump.consume(raw, kernel, truncated); err != nil {
			return errExitLANBPF
		}
		if dump.done {
			if dump.failed || (present && !dump.confirmed()) || (!present && dump.found != 0) {
				return errExitLANBPF
			}
			return ctx.Err()
		}
	}
	return errExitLANBPF
}

type nativeExitLANHookTransport struct {
	file                *os.File
	raw                 syscall.RawConn
	port                uint32
	stop                func() bool
	cancelDone          chan struct{}
	fileOnce, closeOnce sync.Once
	fileErr             error
}

func openNativeExitLANHookTransport(ctx context.Context) (exitLANHookTransport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.NETLINK_NETFILTER)
	if err != nil {
		return nil, errExitLANBPF
	}
	owned := true
	defer func() {
		if owned {
			_ = unix.Close(fd)
		}
	}()
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, errExitLANBPF
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		return nil, errExitLANBPF
	}
	local, ok := address.(*unix.SockaddrNetlink)
	if !ok || local == nil || local.Family != unix.AF_NETLINK || local.Pid == 0 || local.Groups != 0 {
		return nil, errExitLANBPF
	}
	file := os.NewFile(uintptr(fd), "LAN netfilter hook dump")
	if file == nil {
		return nil, errExitLANBPF
	}
	owned = false
	raw, err := file.SyscallConn()
	if err != nil {
		_ = file.Close()
		return nil, errExitLANBPF
	}
	n := &nativeExitLANHookTransport{file: file, raw: raw, port: local.Pid, cancelDone: make(chan struct{})}
	// Closing the pollable nonblocking file wakes Read/Write. RawConn keeps the
	// descriptor referenced while the callback runs, preventing fd reuse races.
	n.stop = context.AfterFunc(ctx, func() { n.closeFile(); close(n.cancelDone) })
	if err := ctx.Err(); err != nil {
		_ = n.Close()
		return nil, err
	}
	return n, nil
}

func (n *nativeExitLANHookTransport) Port() uint32 { return n.port }

func (n *nativeExitLANHookTransport) closeFile() {
	n.fileOnce.Do(func() { n.fileErr = n.file.Close() })
}

func (n *nativeExitLANHookTransport) Close() error {
	n.closeOnce.Do(func() {
		if !n.stop() {
			<-n.cancelDone
		}
		n.closeFile()
	})
	return n.fileErr
}

func (n *nativeExitLANHookTransport) Send(ctx context.Context, request []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(request) == 0 || len(request) > 64<<10 {
		return errExitLANBPF
	}
	var sendErr error
	err := n.raw.Write(func(fd uintptr) bool {
		for {
			if ctx.Err() != nil {
				sendErr = ctx.Err()
				return true
			}
			sendErr = unix.Sendto(int(fd), request, unix.MSG_DONTWAIT, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
			if sendErr == unix.EINTR {
				continue
			}
			return sendErr != unix.EAGAIN && sendErr != unix.EWOULDBLOCK
		}
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || sendErr != nil {
		return errExitLANBPF
	}
	return nil
}

func (n *nativeExitLANHookTransport) Receive(ctx context.Context) ([]byte, bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}
	buffer := make([]byte, 64<<10)
	var count, flags int
	var sender unix.Sockaddr
	var receiveErr error
	err := n.raw.Read(func(fd uintptr) bool {
		for {
			if ctx.Err() != nil {
				receiveErr = ctx.Err()
				return true
			}
			count, _, flags, sender, receiveErr = unix.Recvmsg(int(fd), buffer, nil, unix.MSG_DONTWAIT)
			if receiveErr == unix.EINTR {
				continue
			}
			return receiveErr != unix.EAGAIN && receiveErr != unix.EWOULDBLOCK
		}
	})
	if ctx.Err() != nil {
		return nil, false, false, ctx.Err()
	}
	if err != nil || receiveErr != nil || count <= 0 || count > len(buffer) {
		return nil, false, false, errExitLANBPF
	}
	kernel, truncated := exitLANHookSender(sender, flags)
	return buffer[:count], kernel, truncated, nil
}

func exitLANHookSender(sender unix.Sockaddr, flags int) (bool, bool) {
	peer, ok := sender.(*unix.SockaddrNetlink)
	return ok && peer != nil && peer.Family == unix.AF_NETLINK && peer.Pid == 0 && peer.Groups == 0, flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0
}
