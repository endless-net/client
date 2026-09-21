//go:build linux

package client

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

type exitLANBPFDirectoryTestFS struct {
	t                                         *testing.T
	stats                                     [5]unix.Stat_t
	open, closed                              map[int]bool
	calls, failAt, mkdirs, locks              int
	missing, raced, locked, wrongFS, replaced bool
	lockError                                 error
	after                                     func()
}

func newExitLANBPFDirectoryTestFS(t *testing.T) *exitLANBPFDirectoryTestFS {
	f := &exitLANBPFDirectoryTestFS{t: t, open: map[int]bool{}, closed: map[int]bool{}}
	for i := range f.stats {
		f.stats[i] = unix.Stat_t{Mode: unix.S_IFDIR | 0755, Ino: uint64(i + 1), Dev: 1, Nlink: 2}
	}
	f.stats[3].Mode = unix.S_IFDIR | 01777
	f.stats[3].Dev = 7
	f.stats[4].Mode = unix.S_IFDIR | 0700
	f.stats[4].Dev = 7
	return f
}

func (f *exitLANBPFDirectoryTestFS) step() error {
	f.calls++
	if f.after != nil {
		f.after()
	}
	if f.calls == f.failAt {
		return unix.EIO
	}
	return nil
}

func (f *exitLANBPFDirectoryTestFS) index(parent int, name string) int {
	names := []string{"/", "sys", "fs", "bpf", "endlessnet_lan"}
	for i, candidate := range names {
		wantParent := unix.AT_FDCWD
		if i != 0 {
			wantParent = 10 + i - 1
		}
		if name == candidate && parent == wantParent {
			return i
		}
	}
	f.t.Fatalf("unexpected path operation parent=%d name=%q", parent, name)
	return -1
}

func (f *exitLANBPFDirectoryTestFS) ops() exitLANBPFDirectoryOps {
	return exitLANBPFDirectoryOps{
		openat: func(parent int, name string, flags int, mode uint32) (int, error) {
			if err := f.step(); err != nil {
				return -1, err
			}
			if flags != unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC || mode != 0 {
				f.t.Fatal("unsafe directory open flags")
			}
			i := f.index(parent, name)
			if i == 4 && f.missing {
				return -1, unix.ENOENT
			}
			if f.stats[i].Mode&unix.S_IFMT == unix.S_IFLNK {
				return -1, unix.ELOOP
			}
			f.open[10+i] = true
			return 10 + i, nil
		},
		mkdirat: func(parent int, name string, mode uint32) error {
			if err := f.step(); err != nil {
				return err
			}
			if f.index(parent, name) != 4 || mode != 0700 || !f.missing {
				f.t.Fatal("unsafe mkdir")
			}
			f.mkdirs++
			f.missing = false
			if f.raced {
				return unix.EEXIST
			}
			return nil
		},
		fstat: func(fd int, stat *unix.Stat_t) error {
			if err := f.step(); err != nil {
				return err
			}
			if !f.open[fd] || f.closed[fd] {
				f.t.Fatal("stat of unowned descriptor")
			}
			*stat = f.stats[fd-10]
			return nil
		},
		fstatat: func(parent int, name string, stat *unix.Stat_t, flags int) error {
			if err := f.step(); err != nil {
				return err
			}
			if flags != unix.AT_SYMLINK_NOFOLLOW {
				f.t.Fatal("unsafe path recheck")
			}
			i := f.index(parent, name)
			*stat = f.stats[i]
			if f.replaced && i == 4 {
				stat.Ino++
			}
			return nil
		},
		fstatfs: func(fd int, stat *unix.Statfs_t) error {
			if err := f.step(); err != nil {
				return err
			}
			if !f.open[fd] || f.closed[fd] || fd < 13 {
				f.t.Fatal("unexpected filesystem query")
			}
			stat.Type = unix.BPF_FS_MAGIC
			if f.wrongFS {
				stat.Type = unix.TMPFS_MAGIC
			}
			return nil
		},
		flock: func(fd, flags int) error {
			if err := f.step(); err != nil {
				return err
			}
			if fd != 14 || flags != unix.LOCK_EX|unix.LOCK_NB {
				f.t.Fatal("unsafe lock")
			}
			f.locks++
			if f.lockError != nil {
				return f.lockError
			}
			f.locked = true
			return nil
		},
		close: func(fd int) error {
			if !f.open[fd] || f.closed[fd] {
				f.t.Fatal("foreign or repeated close")
			}
			f.closed[fd] = true
			if fd == 14 {
				f.locked = false
			}
			return nil
		},
	}
}

func (f *exitLANBPFDirectoryTestFS) assertClosed() {
	for fd := range f.open {
		if !f.closed[fd] {
			f.t.Fatal("leaked directory descriptor", fd)
		}
	}
	if f.locked {
		f.t.Fatal("lock survived cleanup")
	}
}

func TestExitLANBPFDirectoryAcquireAndValidate(t *testing.T) {
	for _, mode := range []string{"existing", "created", "mkdir_race"} {
		t.Run(mode, func(t *testing.T) {
			f := newExitLANBPFDirectoryTestFS(t)
			f.missing, f.raced = mode != "existing", mode == "mkdir_race"
			d, err := openExitLANBPFDirectoryWith(t.Context(), f.ops())
			if err != nil {
				t.Fatal(err)
			}
			if !f.locked || f.locks != 1 || len(f.closed) != 0 || d.fd != 14 {
				t.Fatal("directory lifetime not retained")
			}
			if err := d.check(d.fd); err != nil {
				t.Fatal(err)
			}
			if err := d.check(15); err == nil {
				t.Fatal("foreign fd accepted")
			}
			if mode == "existing" && f.mkdirs != 0 || mode != "existing" && f.mkdirs != 1 {
				t.Fatal("unexpected mkdir")
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			f.assertClosed()
		})
	}
}

func TestExitLANBPFDirectoryRejectsUntrustedPaths(t *testing.T) {
	for name, change := range map[string]func(*exitLANBPFDirectoryTestFS){
		"parent symlink":           func(f *exitLANBPFDirectoryTestFS) { f.stats[1].Mode = unix.S_IFLNK | 0777 },
		"owned symlink":            func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Mode = unix.S_IFLNK | 0777 },
		"root not owned":           func(f *exitLANBPFDirectoryTestFS) { f.stats[0].Uid = 1 },
		"parent writable":          func(f *exitLANBPFDirectoryTestFS) { f.stats[2].Mode |= 0020 },
		"bpffs writable nonsticky": func(f *exitLANBPFDirectoryTestFS) { f.stats[3].Mode = unix.S_IFDIR | 0777 },
		"bpffs group":              func(f *exitLANBPFDirectoryTestFS) { f.stats[3].Gid = 1 },
		"owned permissions":        func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Mode |= 0050 },
		"owned user":               func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Uid = 1 },
		"owned setgid":             func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Mode |= unix.S_ISGID },
		"owned unlinked":           func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Nlink = 0 },
		"nested mount":             func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Dev++ },
		"wrong fs":                 func(f *exitLANBPFDirectoryTestFS) { f.wrongFS = true },
		"path replaced":            func(f *exitLANBPFDirectoryTestFS) { f.replaced = true },
		"lock contention":          func(f *exitLANBPFDirectoryTestFS) { f.lockError = unix.EWOULDBLOCK },
		"lock unsupported":         func(f *exitLANBPFDirectoryTestFS) { f.lockError = unix.EOPNOTSUPP },
	} {
		t.Run(name, func(t *testing.T) {
			f := newExitLANBPFDirectoryTestFS(t)
			change(f)
			if d, err := openExitLANBPFDirectoryWith(t.Context(), f.ops()); err == nil || d != nil {
				t.Fatal("untrusted directory accepted")
			}
			f.assertClosed()
		})
	}
}

func TestExitLANBPFDirectoryFailureAndCancellationCleanup(t *testing.T) {
	baseline := newExitLANBPFDirectoryTestFS(t)
	baseline.missing = true
	d, err := openExitLANBPFDirectoryWith(t.Context(), baseline.ops())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.closeFD(d.fd); err != nil {
		t.Fatal(err)
	}
	for at := 1; at <= baseline.calls; at++ {
		for _, cancelled := range []bool{false, true} {
			f := newExitLANBPFDirectoryTestFS(t)
			f.missing = true
			ctx, cancel := context.WithCancel(t.Context())
			if cancelled {
				f.after = func() {
					if f.calls == at {
						cancel()
					}
				}
			} else {
				f.failAt = at
			}
			d, err := openExitLANBPFDirectoryWith(ctx, f.ops())
			cancel()
			if err == nil || d != nil || cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("step=%d cancelled=%t result=%v", at, cancelled, err)
			}
			f.assertClosed()
		}
	}
	f := newExitLANBPFDirectoryTestFS(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openExitLANBPFDirectoryWith(ctx, f.ops()); !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatal("pre-cancel performed operations")
	}
}

func TestExitLANBPFDirectoryReceiptInvalidation(t *testing.T) {
	for _, change := range []func(*exitLANBPFDirectoryTestFS){
		func(f *exitLANBPFDirectoryTestFS) { f.stats[4].Mode |= 0001 },
		func(f *exitLANBPFDirectoryTestFS) { f.stats[3].Uid = 1 },
		func(f *exitLANBPFDirectoryTestFS) { f.stats[1].Ino++ },
		func(f *exitLANBPFDirectoryTestFS) { f.replaced = true },
		func(f *exitLANBPFDirectoryTestFS) { f.wrongFS = true },
	} {
		f := newExitLANBPFDirectoryTestFS(t)
		d, err := openExitLANBPFDirectoryWith(t.Context(), f.ops())
		if err != nil {
			t.Fatal(err)
		}
		change(f)
		if err := d.check(d.fd); err == nil {
			t.Fatal("changed directory remained valid")
		}
		if err := d.closeFD(d.fd); err != nil {
			t.Fatal(err)
		}
		f.assertClosed()
	}
}
