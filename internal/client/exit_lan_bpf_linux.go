//go:build linux

package client

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func callExitLANBPF(command int, attr []byte, buffers ...[]byte) (int, error) {
	if len(attr) == 0 {
		return -1, errExitLANBPF
	}
	r, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(command), uintptr(unsafe.Pointer(&attr[0])), uintptr(len(attr)))
	runtime.KeepAlive(attr)
	runtime.KeepAlive(buffers)
	if errno != 0 {
		if command == exitLANBPFMapDelete && errno == unix.ENOENT {
			return 0, nil
		}
		return -1, errno
	}
	return int(r), nil
}

// Only the kernel's fixed sysfs BTF source is accepted. Objects are loaded but
// never attached or pinned here; ordinary process teardown cannot open LAN.
func newNativeExitLANBPFPreparation(ctx context.Context, mark uint32) (*exitLANBPFPreparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errExitLANBPF
	}
	fd, err := unix.Open("/sys/kernel/btf/vmlinux", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errExitLANBPF
	}
	file := os.NewFile(uintptr(fd), "kernel BTF")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errExitLANBPF
	}
	defer func() { _ = file.Close() }()
	var fs unix.Statfs_t
	if unix.Fstatfs(fd, &fs) != nil || uint64(fs.Type) != unix.SYSFS_MAGIC {
		return nil, errExitLANBPF
	}
	raw, err := io.ReadAll(io.LimitReader(file, exitLANBTFMaxBytes+1))
	if err != nil {
		return nil, errExitLANBPF
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	ctxOffset, markOffset, err := exitLANBTFMarkLayout(raw, 8)
	if err != nil {
		return nil, err
	}
	// binary.NativeEndian is not a canonical ByteOrder identity for the
	// portable encoder. Select its exact host order explicitly.
	var order binary.ByteOrder = binary.LittleEndian
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		order = binary.BigEndian
	}
	return newExitLANBPFPreparation(ctx, mark, ctxOffset, markOffset, unix.BPF_PROG_TYPE_NETFILTER, unix.BPF_NETFILTER, order, callExitLANBPF, unix.Close)
}
