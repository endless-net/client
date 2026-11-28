package dataplane

import (
	"errors"
	"testing"

	"github.com/endless-net/client/internal/underlay"
)

type markingBind struct {
	inertBind
	marks []uint32
	err   error
}

func (b *markingBind) SetMark(mark uint32) error {
	b.marks = append(b.marks, mark)
	return b.err
}

func TestLinuxWireGuardMarksEachOuterBind(t *testing.T) {
	socket := &markingBind{err: ErrConfiguration}
	bind := linuxUnderlayBind{socket}
	if r, port, err := bind.Open(51820); !errors.Is(err, ErrConfiguration) || port != 0 || r != nil || socket.done != nil {
		t.Fatal("unmarked bind remained open", port, err)
	}
	socket.err = nil
	for i := 0; i < 2; i++ {
		if r, port, err := bind.Open(51820); err != nil || port != 51820 || len(r) != 1 {
			t.Fatal(port, err)
		}
		if err := bind.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if len(socket.marks) != 3 {
		t.Fatal(socket.marks)
	}
	for _, mark := range socket.marks {
		if mark != underlay.Mark {
			t.Fatal(mark)
		}
	}
}
