package client

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANBPFLinkTestKernel struct {
	k           *exitLANBPFTestKernel
	links       map[int]exitLANBPFLinkIdentity
	steps, fail int
	after       func(int, []byte, []byte)
}

func (k *exitLANBPFLinkTestKernel) call(command int, attr []byte, buffers ...[]byte) (int, error) {
	k.steps++
	if k.steps == k.fail {
		return -1, errExitLANBPF
	}
	o := k.k.order
	u32 := func(at int) uint32 { return o.Uint32(attr[at:]) }
	fd := 0
	var info []byte
	switch command {
	case exitLANBPFInfo:
		if len(attr) != 16 || len(buffers) != 1 || int(u32(4)) != len(buffers[0]) || o.Uint64(attr[8:]) != exitLANBPFPointer(buffers[0]) {
			k.k.t.Fatal("incorrect INFO layout")
		}
		obj := k.k.objects[int(u32(0))]
		if obj == nil || obj.closed {
			k.k.t.Fatal("INFO of missing object")
		}
		info = buffers[0]
		if obj.kind == 999 {
			if len(info) != 8 {
				k.k.t.Fatal("wrong program identity prefix")
			}
			o.PutUint32(info, 32)
			o.PutUint32(info[4:], 100)
		} else {
			identity, ok := k.links[int(u32(0))]
			if !ok || len(info) != 32 {
				k.k.t.Fatal("wrong link identity prefix")
			}
			for i, value := range []uint32{10, identity.id, identity.programID, 0, identity.family, identity.hook, uint32(identity.priority), 0} {
				o.PutUint32(info[4*i:], value)
			}
		}
	case exitLANBPFLinkCreate:
		if len(attr) != 32 || u32(4) != 0 || u32(8) != 45 || u32(12) != 0 || u32(28) != 0 || (u32(16) != 2 && u32(16) != 10) {
			k.k.t.Fatal("incorrect LINK_CREATE layout")
		}
		if obj := k.k.objects[int(u32(0))]; obj == nil || obj.closed || obj.kind != 999 {
			k.k.t.Fatal("wrong linked program")
		}
		fd = k.k.next
		k.k.next++
		k.k.objects[fd] = &exitLANBPFTestObject{kind: 888}
		k.links[fd] = exitLANBPFLinkIdentity{uint32(fd + 100), 100, u32(16), u32(20), int32(u32(24))}
	default:
		var err error
		fd, err = k.k.call(command, attr, buffers...)
		if err != nil {
			return fd, err
		}
	}
	if k.after != nil {
		k.after(command, attr, info)
	}
	return fd, nil
}

func newExitLANBPFLinkFixture(t *testing.T, order binary.ByteOrder) (*exitLANBPFPreparation, *exitLANBPFLinkTestKernel) {
	t.Helper()
	k := newExitLANBPFTestKernel(t, order)
	p, err := newExitLANBPFPreparation(t.Context(), 7, 8, 64, 32, 45, order, k.call, k.close)
	if err != nil {
		t.Fatal(err)
	}
	l := &exitLANBPFLinkTestKernel{k: k, links: map[int]exitLANBPFLinkIdentity{}}
	p.call = l.call
	return p, l
}

func TestExitLANBPFLinksAttachClosedAndRetainIdentity(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		p, k := newExitLANBPFLinkFixture(t, order)
		// Existing lease is withdrawn before either family is attached.
		d, clock := exitLANBPFTestDeadline(t)
		if err := p.publishBootDeadline(t.Context(), d, clock); err != nil {
			t.Fatal(err)
		}
		if err := p.attachClosed(t.Context(), api.ExitFamilyDualStack, 4, -100); err != nil {
			t.Fatal(err)
		}
		if len(p.links) != 2 || k.k.objects[p.outer].slot != 0 {
			t.Fatal("attachment retained open lease")
		}
		for i, family := range []uint32{2, 10} {
			got := p.links[i]
			if got.identity.family != family || got.identity.hook != 4 || got.identity.priority != -100 || got.identity.programID != 100 || k.k.objects[got.fd].closed {
				t.Fatal("unbound or prematurely closed link")
			}
		}
		before := k.steps
		if err := p.attachClosed(t.Context(), api.ExitFamilyDualStack, 4, -100); err == nil || k.steps != before {
			t.Fatal("duplicate attach mutated owned links")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.k.assertClosed()
	}
}

func TestExitLANBPFLinkFailureAndCancellationCleanup(t *testing.T) {
	for step := 1; step <= 6; step++ {
		for _, cancelAfter := range []bool{false, true} {
			p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
			ctx, cancel := context.WithCancel(t.Context())
			if cancelAfter {
				k.after = func(int, []byte, []byte) {
					if k.steps == step {
						cancel()
					}
				}
			} else {
				k.fail = step
			}
			err := p.attachClosed(ctx, api.ExitFamilyDualStack, 4, 10)
			cancel()
			if err == nil || (cancelAfter && !errors.Is(err, context.Canceled)) || len(p.links) != 0 {
				t.Fatal("failed attach retained links", step, cancelAfter, err)
			}
			for fd := range k.links {
				if !k.k.objects[fd].closed {
					t.Fatal("partial family link leaked")
				}
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			k.k.assertClosed()
		}
	}
}

func TestExitLANBPFLinkRejectsMismatchedReadback(t *testing.T) {
	for _, field := range []int{0, 4, 8, 12, 16, 20, 24, 28, -1} {
		p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
		k.after = func(command int, attr, info []byte) {
			if command != exitLANBPFInfo || len(info) != 32 {
				return
			}
			if field < 0 {
				p.order.PutUint32(attr[4:], 31)
				return
			}
			value := p.order.Uint32(info[field:]) + 1
			if field == 4 {
				value = 0
			}
			p.order.PutUint32(info[field:], value)
		}
		if err := p.attachClosed(t.Context(), api.ExitFamilyDualStack, 4, 10); err == nil || len(p.links) != 0 {
			t.Fatal("bad link identity accepted", field)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.k.assertClosed()
	}
}

func TestExitLANBPFLinkRejectsUnsafeInputs(t *testing.T) {
	p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
	defer func() { _ = p.Close() }()
	for _, args := range []struct {
		hook     uint32
		priority int32
	}{{5, 0}, {4, math.MinInt32}, {4, math.MaxInt32}} {
		if err := p.attachClosed(t.Context(), api.ExitFamilyDualStack, args.hook, args.priority); err == nil || k.steps != 0 {
			t.Fatal("invalid hook reached syscall")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.attachClosed(ctx, api.ExitFamilyDualStack, 4, 0); !errors.Is(err, context.Canceled) || k.steps != 0 {
		t.Fatal("pre-cancel touched objects")
	}
}

func TestExitLANBPFLinksRejectDuplicateAndForeignProgramIdentity(t *testing.T) {
	for _, scenario := range []string{"duplicate_link", "program_type", "program_zero", "program_short"} {
		p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
		k.after = func(command int, attr, info []byte) {
			if command != exitLANBPFInfo {
				return
			}
			if scenario == "duplicate_link" && len(info) == 32 {
				p.order.PutUint32(info[4:], 777)
			}
			if len(info) == 8 {
				switch scenario {
				case "program_type":
					p.order.PutUint32(info, 1)
				case "program_zero":
					p.order.PutUint32(info[4:], 0)
				case "program_short":
					p.order.PutUint32(attr[4:], 7)
				}
			}
		}
		if err := p.attachClosed(t.Context(), api.ExitFamilyDualStack, 4, 10); err == nil || len(p.links) != 0 {
			t.Fatal("bad object identity accepted", scenario)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.k.assertClosed()
	}
}

func TestExitLANBPFLinksUseOnlySelectedFamilies(t *testing.T) {
	for _, tc := range []struct {
		mode        api.ExitFamilyMode
		unavailable uint32
		want        []uint32
	}{
		{api.ExitFamilyIPv4Only, 10, []uint32{2}},
		{api.ExitFamilyIPv6Only, 2, []uint32{10}},
		{api.ExitFamilyDualStack, 0, []uint32{2, 10}},
		{api.ExitFamilyDualStack, 10, nil},
		{api.ExitFamilyIPv4Only, 2, nil},
		{api.ExitFamilyIPv6Only, 10, nil},
	} {
		p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
		original := p.call
		p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
			if command == exitLANBPFLinkCreate && p.order.Uint32(attr[16:]) == tc.unavailable {
				return -1, errExitLANBPF
			}
			return original(command, attr, buffers...)
		}
		err := p.attachClosed(t.Context(), tc.mode, 4, 10)
		if len(tc.want) == 0 {
			if err == nil || len(p.links) != 0 || p.family != "" {
				t.Fatal("selected-family failure downgraded mode", tc.mode)
			}
		} else {
			if err != nil || len(p.links) != len(tc.want) || p.family != tc.mode {
				t.Fatal("unselected family blocked attachment", tc.mode, err)
			}
			for i, family := range tc.want {
				if p.links[i].identity.family != family {
					t.Fatal("wrong attached family")
				}
			}
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.k.assertClosed()
	}
	p, k := newExitLANBPFLinkFixture(t, binary.LittleEndian)
	if err := p.attachClosed(t.Context(), "", 4, 10); err == nil || k.steps != 0 {
		t.Fatal("unspecified family reached kernel")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	k.k.assertClosed()
}
