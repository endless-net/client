//go:build linux

package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type exitLANNamespaceTestFD struct {
	path   string
	st     unix.Stat_t
	offset int
	closed bool
}
type exitLANNamespaceTestSystem struct {
	t                           *testing.T
	fds                         map[int]*exitLANNamespaceTestFD
	next, steps, fail, cancelAt int
	cancel                      context.CancelFunc
	boot                        string
	nsInode                     uint64
	bad                         string
}

func newExitLANNamespaceTestSystem(t *testing.T) *exitLANNamespaceTestSystem {
	return &exitLANNamespaceTestSystem{t: t, fds: map[int]*exitLANNamespaceTestFD{}, next: 10, boot: "12345678-9abc-4def-8123-456789abcdef\n", nsInode: 77}
}

func (s *exitLANNamespaceTestSystem) step() bool {
	s.steps++
	if s.steps == s.cancelAt && s.cancel != nil {
		s.cancel()
	}
	return s.steps == s.fail
}
func (s *exitLANNamespaceTestSystem) path(parent int, name string) string {
	if parent == unix.AT_FDCWD {
		return name
	}
	return strings.TrimSuffix(s.fds[parent].path, "/") + "/" + name
}
func (s *exitLANNamespaceTestSystem) stat(path string) unix.Stat_t {
	kind := uint32(unix.S_IFDIR | 0555)
	dev, ino := uint64(2), uint64(20)
	if path == "/" {
		dev = 1
		ino = 1
	}
	for _, c := range path {
		ino += uint64(c)
	}
	if strings.HasSuffix(path, "/net") {
		kind = unix.S_IFREG | 0444
		dev = 3
		ino = s.nsInode
	}
	if strings.HasSuffix(path, "/boot_id") {
		kind = unix.S_IFREG | 0444
	}
	st := unix.Stat_t{Mode: kind, Dev: dev, Ino: ino, Nlink: 1}
	if s.bad == "permissions" && path == "/proc/sys" {
		st.Mode |= 0020
	}
	if s.bad == "owner" && path == "/proc" {
		st.Uid = 1000
	}
	if s.bad == "boot_writable" && strings.HasSuffix(path, "/boot_id") {
		st.Mode |= 0020
	}
	return st
}

func (s *exitLANNamespaceTestSystem) ops() exitLANNamespaceOps {
	return exitLANNamespaceOps{
		openat: func(parent int, name string, flags int, _ uint32) (int, error) {
			if s.step() {
				return -1, unix.EIO
			}
			path := s.path(parent, name)
			allowed := path == "/" || path == "/proc" || path == "/proc/sys" || path == "/proc/sys/kernel" || path == "/proc/sys/kernel/random" || path == "/proc/sys/kernel/random/boot_id" || path == "/proc/thread-self" || path == "/proc/thread-self/ns" || path == "/proc/thread-self/ns/net"
			if !allowed {
				s.t.Fatalf("unexpected path %q", path)
			}
			want := unix.O_RDONLY | unix.O_CLOEXEC
			if !strings.HasSuffix(path, "/net") && path != "/proc/thread-self" {
				want |= unix.O_NOFOLLOW
			}
			if !strings.HasSuffix(path, "/net") && !strings.HasSuffix(path, "/boot_id") {
				want |= unix.O_DIRECTORY
			}
			if flags != want {
				s.t.Fatalf("wrong flags for %s: %x", path, flags)
			}
			if s.bad == "symlink" && path == "/proc/sys" {
				return -1, unix.ELOOP
			}
			fd := s.next
			s.next++
			s.fds[fd] = &exitLANNamespaceTestFD{path: path, st: s.stat(path)}
			return fd, nil
		},
		fstat: func(fd int, st *unix.Stat_t) error {
			if s.step() {
				return unix.EIO
			}
			f := s.fds[fd]
			if f == nil || f.closed {
				s.t.Fatal("stat closed/missing fd")
			}
			*st = f.st
			return nil
		},
		fstatat: func(parent int, name string, st *unix.Stat_t, flags int) error {
			if s.step() {
				return unix.EIO
			}
			if flags != unix.AT_SYMLINK_NOFOLLOW {
				s.t.Fatal("following ancestor stat")
			}
			*st = s.stat(s.path(parent, name))
			if s.bad == "pathswap" && name == "proc" {
				st.Ino++
			}
			return nil
		},
		fstatfs: func(fd int, fs *unix.Statfs_t) error {
			if s.step() {
				return unix.EIO
			}
			path := s.fds[fd].path
			fs.Type = unix.PROC_SUPER_MAGIC
			if strings.HasSuffix(path, "/net") {
				fs.Type = unix.NSFS_MAGIC
			}
			if s.bad == "procfs" && path == "/proc" || s.bad == "nsfs" && strings.HasSuffix(path, "/net") || s.bad == "bootfs" && strings.HasSuffix(path, "/boot_id") {
				fs.Type = 0
			}
			return nil
		},
		ioctl: func(fd int, request uint) (int, error) {
			if s.step() {
				return 0, unix.EIO
			}
			if request != unix.NS_GET_NSTYPE || !strings.HasSuffix(s.fds[fd].path, "/net") {
				s.t.Fatal("invalid namespace ioctl")
			}
			if s.bad == "nstype" {
				return unix.CLONE_NEWUSER, nil
			}
			return unix.CLONE_NEWNET, nil
		},
		read: func(fd int, b []byte) (int, error) {
			if s.step() {
				return 0, unix.EIO
			}
			f := s.fds[fd]
			if !strings.HasSuffix(f.path, "/boot_id") || len(b) > 38 {
				s.t.Fatal("unbounded/unexpected read")
			}
			if f.offset >= len(s.boot) {
				return 0, nil
			}
			n := copy(b, s.boot[f.offset:])
			f.offset += n
			return n, nil
		},
		close: func(fd int) error {
			failed := s.step()
			f := s.fds[fd]
			if f == nil || f.closed {
				s.t.Fatal("double/foreign close", fd)
			}
			f.closed = true
			if failed {
				return unix.EIO
			}
			return nil
		},
	}
}

func (s *exitLANNamespaceTestSystem) allClosed() {
	s.t.Helper()
	for fd, f := range s.fds {
		if !f.closed {
			s.t.Fatal("leaked fd", fd, f.path)
		}
	}
}

func TestExitLANNamespaceNativeCaptureRetainsAndRechecks(t *testing.T) {
	s := newExitLANNamespaceTestSystem(t)
	captureCtx, cancel := context.WithCancel(t.Context())
	n, err := captureExitLANBootNamespaceWith(captureCtx, s.ops())
	if err != nil {
		t.Fatal(err)
	}
	cancel() // An old capture context must not invalidate a later operation.
	called := false
	err = n.withCurrent(t.Context(), func(_ context.Context, id exitLANNamespaceIdentity) error {
		called = true
		if id.BootID != strings.TrimSuffix(s.boot, "\n") || id.Device != 3 || id.Inode != 77 {
			return errExitLANBPF
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal("recheck failed", err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	s.allClosed()
}

func TestExitLANNamespaceNativeRejectsUntrustedFiles(t *testing.T) {
	for _, bad := range []string{"permissions", "owner", "boot_writable", "symlink", "pathswap", "procfs", "nsfs", "bootfs", "nstype"} {
		t.Run(bad, func(t *testing.T) {
			s := newExitLANNamespaceTestSystem(t)
			s.bad = bad
			n, err := captureExitLANBootNamespaceWith(t.Context(), s.ops())
			if err == nil || n != nil {
				if n != nil {
					_ = n.Close()
				}
				t.Fatal("untrusted namespace accepted")
			}
			s.allClosed()
		})
	}
	for _, boot := range []string{"", "00000000-0000-0000-0000-000000000000\n", "12345678-9ABC-4def-8123-456789abcdef\n", "12345678-9abc-4def-8123-456789abcdef", "12345678-9abc-4def-8123-456789abcdef\nX", strings.Repeat("a", 100)} {
		s := newExitLANNamespaceTestSystem(t)
		s.boot = boot
		n, err := captureExitLANBootNamespaceWith(t.Context(), s.ops())
		if err == nil || n != nil {
			if n != nil {
				_ = n.Close()
			}
			t.Fatal("invalid boot accepted")
		}
		s.allClosed()
	}
}

func TestExitLANNamespaceNativeChangedIdentityCannotRunCallback(t *testing.T) {
	for _, change := range []string{"boot", "namespace", "mount"} {
		t.Run(change, func(t *testing.T) {
			s := newExitLANNamespaceTestSystem(t)
			n, err := captureExitLANBootNamespaceWith(t.Context(), s.ops())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "boot":
				s.boot = "22345678-9abc-4def-8123-456789abcdef\n"
			case "namespace":
				s.nsInode++
			case "mount":
				s.bad = "pathswap"
			}
			called := false
			err = n.withCurrent(t.Context(), func(context.Context, exitLANNamespaceIdentity) error { called = true; return nil })
			if err == nil || called {
				t.Fatal("changed identity ran effects")
			}
			_ = n.Close()
			s.allClosed()
		})
	}
}

func TestExitLANNamespaceNativeFailureAndCancellationCloseAll(t *testing.T) {
	baseline := newExitLANNamespaceTestSystem(t)
	n, err := captureExitLANBootNamespaceWith(t.Context(), baseline.ops())
	if err != nil {
		t.Fatal(err)
	}
	steps := baseline.steps
	_ = n.Close()
	for _, canceling := range []bool{false, true} {
		for at := 1; at <= steps; at++ {
			s := newExitLANNamespaceTestSystem(t)
			ctx, cancel := context.WithCancel(t.Context())
			s.cancel = cancel
			if canceling {
				s.cancelAt = at
			} else {
				s.fail = at
			}
			n, err := captureExitLANBootNamespaceWith(ctx, s.ops())
			cancel()
			if n != nil {
				_ = n.Close()
			}
			s.allClosed()
			if err == nil {
				t.Fatal("injected syscall failure was ignored", at)
			}
			if canceling && !errors.Is(err, context.Canceled) {
				t.Fatal("lost injected cancellation", at, err)
			}
		}
	}
	s := newExitLANNamespaceTestSystem(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	n, err = captureExitLANBootNamespaceWith(ctx, s.ops())
	if n != nil || !errors.Is(err, context.Canceled) || s.steps != 0 {
		t.Fatal("pre-canceled capture touched OS")
	}
}
