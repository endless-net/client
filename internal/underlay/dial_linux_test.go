package underlay

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type rawSocket struct {
	syscall.RawConn
	controlErr error
}

func (s rawSocket) Control(f func(uintptr)) error {
	if s.controlErr == nil {
		f(42)
	}
	return s.controlErr
}

func TestLinuxUnderlayMarkBeforeDial(t *testing.T) {
	d := Dialer(time.Second)
	if d.Timeout != time.Second || d.Control == nil || d.Resolver == nil || !d.Resolver.PreferGo || d.Resolver.Dial == nil {
		t.Fatal("missing underlay control or DNS dialer")
	}
	var calls int
	set := func(fd, level, option, value int) error {
		calls++
		if fd != 42 || level != unix.SOL_SOCKET || option != unix.SO_MARK || value != Mark {
			t.Fatal(fd, level, option, value)
		}
		return unix.EPERM
	}
	if err := markSocket(rawSocket{}, set); !errors.Is(err, unix.EPERM) {
		t.Fatal(err)
	}
	if err := markSocket(rawSocket{controlErr: unix.EBADF}, set); !errors.Is(err, unix.EBADF) || calls != 1 {
		t.Fatal(err, calls)
	}
}
