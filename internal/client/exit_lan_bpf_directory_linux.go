//go:build linux

package client

import (
	"context"
	"errors"

	"golang.org/x/sys/unix"
)

type exitLANBPFDirectoryOps struct {
	openat  func(int, string, int, uint32) (int, error)
	mkdirat func(int, string, uint32) error
	fstat   func(int, *unix.Stat_t) error
	fstatat func(int, string, *unix.Stat_t, int) error
	fstatfs func(int, *unix.Statfs_t) error
	flock   func(int, int) error
	close   func(int) error
}

func newNativeExitLANBPFDirectory(ctx context.Context) (*exitLANBPFDirectory, error) {
	return openExitLANBPFDirectoryWith(ctx, exitLANBPFDirectoryOps{
		openat: unix.Openat, mkdirat: unix.Mkdirat, fstat: unix.Fstat,
		fstatat: unix.Fstatat, fstatfs: unix.Fstatfs, flock: unix.Flock, close: unix.Close,
	})
}

// The mount must already exist. No mount, chmod, chown, unlink or lock-file
// fallback is permitted. A root-owned sticky bpffs root may be writable by
// other users: the sticky rule prevents them replacing our root-owned child.
// Administrators capable of changing mounts/root-owned paths remain trusted.
func openExitLANBPFDirectoryWith(ctx context.Context, ops exitLANBPFDirectoryOps) (directory *exitLANBPFDirectory, result error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ops.openat == nil || ops.mkdirat == nil || ops.fstat == nil || ops.fstatat == nil || ops.fstatfs == nil || ops.flock == nil || ops.close == nil {
		return nil, errExitLANBPF
	}
	names := []string{"/", "sys", "fs", "bpf", "endlessnet_lan"}
	fds := make([]int, 0, len(names))
	identities := make([]unix.Stat_t, 0, len(names))
	closeAll := func() error {
		var err error
		for i := len(fds) - 1; i >= 0; i-- {
			err = errors.Join(err, ops.close(fds[i]))
		}
		return err
	}
	defer func() {
		if result != nil {
			result = errors.Join(ctx.Err(), result, closeAll())
		}
	}()
	const flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	for index, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent := unix.AT_FDCWD
		if index != 0 {
			parent = fds[index-1]
		}
		fd, err := ops.openat(parent, name, flags, 0)
		if index == 4 && errors.Is(err, unix.ENOENT) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := ops.mkdirat(parent, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
				return nil, errors.Join(errExitLANBPF, err)
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fd, err = ops.openat(parent, name, flags, 0)
		}
		if err != nil {
			return nil, errors.Join(errExitLANBPF, err)
		}
		if fd < 0 {
			return nil, errExitLANBPF
		}
		fds = append(fds, fd)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var stat unix.Stat_t
		if err := ops.fstat(fd, &stat); err != nil {
			return nil, errors.Join(errExitLANBPF, err)
		}
		if !exitLANBPFDirectoryMode(stat, index) {
			return nil, errExitLANBPF
		}
		if index >= 3 {
			var fs unix.Statfs_t
			if err := ops.fstatfs(fd, &fs); err != nil {
				return nil, errors.Join(errExitLANBPF, err)
			}
			if uint64(fs.Type) != unix.BPF_FS_MAGIC || index == 4 && stat.Dev != identities[3].Dev {
				return nil, errExitLANBPF
			}
		}
		identities = append(identities, stat)
	}
	owned := fds[4]
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// bpffs directories use simple_dir_operations; Linux flock supplies the
	// generic inode lock when f_op->flock is absent (kernel/bpf/inode.c,
	// fs/locks.c). O_RDONLY is sufficient. Unsupported/contended locks fail;
	// no blocking syscall or polling goroutine survives context cancellation.
	if err := ops.flock(owned, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errExitLANBPF, err)
	}
	check := func(fd int) error {
		if fd != owned {
			return errExitLANBPF
		}
		for index, currentFD := range fds {
			var stat, pathStat unix.Stat_t
			if err := ops.fstat(currentFD, &stat); err != nil {
				return errors.Join(errExitLANBPF, err)
			}
			if !exitLANBPFDirectoryMode(stat, index) || stat.Dev != identities[index].Dev || stat.Ino != identities[index].Ino {
				return errExitLANBPF
			}
			parent := unix.AT_FDCWD
			if index != 0 {
				parent = fds[index-1]
			}
			if err := ops.fstatat(parent, names[index], &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return errors.Join(errExitLANBPF, err)
			}
			if !exitLANBPFDirectoryMode(pathStat, index) || pathStat.Dev != stat.Dev || pathStat.Ino != stat.Ino {
				return errExitLANBPF
			}
			if index >= 3 {
				var fs unix.Statfs_t
				if err := ops.fstatfs(currentFD, &fs); err != nil {
					return errors.Join(errExitLANBPF, err)
				}
				if uint64(fs.Type) != unix.BPF_FS_MAGIC {
					return errExitLANBPF
				}
			}
		}
		return nil
	}
	if err := check(owned); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &exitLANBPFDirectory{fd: owned, check: check, closeFD: func(fd int) error {
		if fd != owned {
			return errExitLANBPF
		}
		// Closing the locked open-file description releases flock; ancestor
		// descriptors retained for path identity checks are closed afterwards.
		return closeAll()
	}}, nil
}

func exitLANBPFDirectoryMode(stat unix.Stat_t, index int) bool {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink == 0 {
		return false
	}
	permissions := stat.Mode & 07777
	if index == 4 {
		return permissions == 0700
	}
	if permissions&06000 != 0 {
		return false
	}
	if permissions&0022 == 0 {
		return true
	}
	return index == 3 && permissions&unix.S_ISVTX != 0
}
