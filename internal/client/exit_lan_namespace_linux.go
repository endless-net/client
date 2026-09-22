//go:build linux

package client

import (
	"context"
	"errors"
	"runtime"

	"sync"

	"golang.org/x/sys/unix"
)

type exitLANNamespaceOps struct {
	openat  func(int, string, int, uint32) (int, error)
	fstat   func(int, *unix.Stat_t) error
	fstatat func(int, string, *unix.Stat_t, int) error
	fstatfs func(int, *unix.Statfs_t) error
	ioctl   func(int, uint) (int, error)
	read    func(int, []byte) (int, error)
	close   func(int) error
}

func newNativeExitLANBootNamespace(ctx context.Context) (*exitLANBootNamespace, error) {
	return captureExitLANBootNamespaceWith(ctx, exitLANNamespaceOps{openat: unix.Openat, fstat: unix.Fstat, fstatat: unix.Fstatat, fstatfs: unix.Fstatfs, ioctl: unix.IoctlRetInt, read: unix.Read, close: unix.Close})
}

// No setns is performed. The kernel thread-self magic link resolves the
// calling thread in this proc mount's PID namespace; a numeric Gettid path
// could name another task. Follow only thread-self and final ns/net magic
// links, verifying procfs directories and the resulting network nsfs FD.
// Trusted privileged mount administrators remain outside this path validation.
func captureExitLANBootNamespaceWith(ctx context.Context, ops exitLANNamespaceOps) (result *exitLANBootNamespace, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ops.openat == nil || ops.fstat == nil || ops.fstatat == nil || ops.fstatfs == nil || ops.ioctl == nil || ops.read == nil || ops.close == nil {
		return nil, errExitLANBPF
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const directoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	names := []string{"/", "proc", "sys", "kernel", "random"}
	fds := []int{}
	stats := []unix.Stat_t{}
	var once sync.Once
	var closeErr error
	closeAll := func() error {
		once.Do(func() {
			for i := len(fds) - 1; i >= 0; i-- {
				closeErr = errors.Join(closeErr, ops.close(fds[i]))
			}
		})
		return closeErr
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(ctx.Err(), resultErr, closeAll())
		}
	}()
	statFD := func(fd int, magic int64, dir bool) (unix.Stat_t, error) {
		var st unix.Stat_t
		if err := ctx.Err(); err != nil {
			return st, err
		}
		if err := ops.fstat(fd, &st); err != nil {
			return st, errExitLANBPF
		}
		kind := uint32(unix.S_IFREG)
		if dir {
			kind = unix.S_IFDIR
		}
		if st.Mode&unix.S_IFMT != kind || st.Ino == 0 || st.Dev == 0 {
			return st, errExitLANBPF
		}
		if magic != 0 {
			var fs unix.Statfs_t
			if err := ops.fstatfs(fd, &fs); err != nil || int64(fs.Type) != magic {
				return st, errExitLANBPF
			}
		}
		return st, ctx.Err()
	}
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent := unix.AT_FDCWD
		if i > 0 {
			parent = fds[i-1]
		}
		fd, err := ops.openat(parent, name, directoryFlags, 0)
		if err != nil || fd < 0 {
			return nil, errExitLANBPF
		}
		fds = append(fds, fd)
		magic := int64(0)
		if i > 0 {
			magic = unix.PROC_SUPER_MAGIC
		}
		st, err := statFD(fd, magic, true)
		if err != nil {
			return nil, err
		}
		if st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 || i > 1 && st.Dev != stats[1].Dev {
			return nil, errExitLANBPF
		}
		stats = append(stats, st)
	}
	procFD, randomFD := fds[1], fds[4]
	// The closure uses its invocation context, not the capture context: a held
	// identity can outlive the operation that originally captured it.
	checkNamespace := func(checkCtx context.Context, fd int) (unix.Stat_t, error) {
		var st unix.Stat_t
		var fs unix.Statfs_t
		if err := checkCtx.Err(); err != nil {
			return st, err
		}
		if ops.fstat(fd, &st) != nil || ops.fstatfs(fd, &fs) != nil || uint64(fs.Type) != unix.NSFS_MAGIC || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Ino == 0 || st.Dev == 0 {
			return st, errExitLANBPF
		}
		kind, err := ops.ioctl(fd, unix.NS_GET_NSTYPE)
		if err != nil || kind != unix.CLONE_NEWNET {
			return st, errExitLANBPF
		}
		return st, checkCtx.Err()
	}
	openCurrent := func(checkCtx context.Context) (fd int, resultErr error) {
		fd = -1
		if err := checkCtx.Err(); err != nil {
			return -1, err
		}

		parent := procFD
		var opened []int
		defer func() {
			for i := len(opened) - 1; i >= 0; i-- {
				resultErr = errors.Join(resultErr, ops.close(opened[i]))
			}
			if resultErr != nil && fd >= 0 {
				resultErr = errors.Join(resultErr, ops.close(fd))
				fd = -1
			}
		}()
		for _, name := range []string{"thread-self", "ns"} {
			if err := checkCtx.Err(); err != nil {
				return -1, err
			}
			flags := directoryFlags
			if name == "thread-self" {
				flags &^= unix.O_NOFOLLOW
			}
			next, err := ops.openat(parent, name, flags, 0)
			if err != nil || next < 0 {
				return -1, errExitLANBPF
			}
			opened = append(opened, next)
			var st unix.Stat_t
			var fs unix.Statfs_t
			if ops.fstat(next, &st) != nil || ops.fstatfs(next, &fs) != nil || uint64(fs.Type) != unix.PROC_SUPER_MAGIC || st.Dev != stats[1].Dev || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0022 != 0 {
				return -1, errExitLANBPF
			}
			parent = next
		}
		var err error
		fd, err = ops.openat(parent, "net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil || fd < 0 {
			return -1, errExitLANBPF
		}
		if _, err = checkNamespace(checkCtx, fd); err != nil {
			return fd, err
		}
		return fd, checkCtx.Err()
	}
	held, err := openCurrent(ctx)
	if err != nil {
		return nil, err
	}
	fds = append(fds, held)
	heldStat, err := checkNamespace(ctx, held)
	if err != nil {
		return nil, err
	}
	inspect := func(checkCtx context.Context) (identity exitLANNamespaceIdentity, resultErr error) {
		if err := checkCtx.Err(); err != nil {
			return identity, err
		}
		for i, name := range names {
			if err := checkCtx.Err(); err != nil {
				return identity, err
			}
			parent := unix.AT_FDCWD
			if i > 0 {
				parent = fds[i-1]
			}
			var st, path unix.Stat_t
			var fs unix.Statfs_t
			if ops.fstat(fds[i], &st) != nil || ops.fstatat(parent, name, &path, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Dev != stats[i].Dev || st.Ino != stats[i].Ino || path.Dev != st.Dev || path.Ino != st.Ino || path.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
				return identity, errExitLANBPF
			}
			if i > 0 && (ops.fstatfs(fds[i], &fs) != nil || uint64(fs.Type) != unix.PROC_SUPER_MAGIC) {
				return identity, errExitLANBPF
			}
		}
		current, err := openCurrent(checkCtx)
		if err != nil {
			return identity, err
		}
		defer func() {
			resultErr = errors.Join(resultErr, ops.close(current))
			if checkCtx.Err() != nil {
				resultErr = checkCtx.Err()
			}
		}()
		actual, err := checkNamespace(checkCtx, current)
		if err != nil {
			return identity, err
		}
		original, err := checkNamespace(checkCtx, held)
		if err != nil {
			return identity, err
		}
		if actual.Dev != heldStat.Dev || actual.Ino != heldStat.Ino || original.Dev != heldStat.Dev || original.Ino != heldStat.Ino {
			return identity, errExitLANBPF
		}
		bootFD, err := ops.openat(randomFD, "boot_id", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil || bootFD < 0 {
			return identity, errExitLANBPF
		}
		defer func() { resultErr = errors.Join(resultErr, ops.close(bootFD)) }()
		var st unix.Stat_t
		var fs unix.Statfs_t
		if ops.fstat(bootFD, &st) != nil || ops.fstatfs(bootFD, &fs) != nil || uint64(fs.Type) != unix.PROC_SUPER_MAGIC || st.Dev != stats[1].Dev || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
			return identity, errExitLANBPF
		}
		buffer := make([]byte, 38)
		total := 0
		for reads := 0; reads < 40; reads++ {
			if err := checkCtx.Err(); err != nil {
				return identity, err
			}
			n, err := ops.read(bootFD, buffer[total:])
			if err != nil || n < 0 || n > len(buffer)-total {
				return identity, errExitLANBPF
			}
			total += n
			if n == 0 {
				if total != 37 || buffer[36] != '\n' || !exitLANOwnershipBootID(string(buffer[:36])) {
					return identity, errExitLANBPF
				}
				return exitLANNamespaceIdentity{BootID: string(buffer[:36]), Device: uint64(actual.Dev), Inode: actual.Ino}, checkCtx.Err()
			}
			if total == len(buffer) {
				return identity, errExitLANBPF
			}
		}
		return identity, errExitLANBPF
	}
	return newExitLANBootNamespace(ctx, inspect, closeAll)
}
