package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitLANBPFTestScope = "0123456789abcdef01234567"

type exitLANBPFPinTestKernel struct {
	link        *exitLANBPFLinkTestKernel
	pins        map[string]int
	steps, fail int
	after       func(int)
}

func (k *exitLANBPFPinTestKernel) call(command int, attr []byte, buffers ...[]byte) (int, error) {
	k.steps++
	if k.steps == k.fail {
		return -1, errExitLANBPF
	}
	defer func() {
		if k.after != nil {
			k.after(command)
		}
	}()
	base := k.link.k
	order := base.order
	if command == exitLANBPFInfo && len(buffers) == 1 && len(buffers[0]) == 8 && base.objects[int(order.Uint32(attr))].kind == exitLANBPFArrayOfMaps {
		if len(attr) != 16 || order.Uint32(attr[4:]) != 8 || order.Uint64(attr[8:]) != exitLANBPFPointer(buffers[0]) {
			base.t.Fatal("bad map INFO prefix")
		}
		order.PutUint32(buffers[0], exitLANBPFArrayOfMaps)
		order.PutUint32(buffers[0][4:], 200)
		return 0, nil
	}
	if command != exitLANBPFObjectPin && command != exitLANBPFObjectGet {
		return k.link.call(command, attr, buffers...)
	}
	if len(attr) != 20 || len(buffers) != 1 || order.Uint64(attr) != exitLANBPFPointer(buffers[0]) || order.Uint32(attr[12:]) != exitLANBPFPathFD || order.Uint32(attr[16:]) != 800 {
		base.t.Fatal("bad FD-relative object command")
	}
	path := buffers[0]
	if len(path) < 2 || path[len(path)-1] != 0 || strings.ContainsRune(string(path[:len(path)-1]), 0) {
		base.t.Fatal("bad pin path")
	}
	name := string(path[:len(path)-1])
	fd := int(order.Uint32(attr[8:]))
	if command == exitLANBPFObjectPin {
		if _, exists := k.pins[name]; exists {
			return -1, fs.ErrExist
		}
		if obj := base.objects[fd]; obj == nil || obj.closed {
			base.t.Fatal("pin of closed object")
		}
		k.pins[name] = fd
		return 0, nil
	}
	if fd != 0 {
		base.t.Fatal("GET used an input object FD")
	}
	original, exists := k.pins[name]
	if !exists {
		return -1, fs.ErrNotExist
	}
	fd = base.next
	base.next++
	copy := *base.objects[original]
	copy.closed = false
	base.objects[fd] = &copy
	if identity, ok := k.link.links[original]; ok {
		k.link.links[fd] = identity
	}
	return fd, nil
}

// ABI tests isolate pin creation from the durable checkpoint tested separately.
func pinExitLANBPFTestClosed(p *exitLANBPFPreparation, ctx context.Context, directory *exitLANBPFDirectory, scope string) ([]string, error) {
	return p.pinClosedWithCheckpoint(ctx, directory, scope, func() error { return nil })
}

func newExitLANBPFPinFixture(t *testing.T, order binary.ByteOrder, mode api.ExitFamilyMode) (*exitLANBPFPreparation, *exitLANBPFDirectory, *exitLANBPFPinTestKernel) {
	t.Helper()
	p, link := newExitLANBPFLinkFixture(t, order)
	if err := p.attachClosed(t.Context(), mode, 4, 10); err != nil {
		t.Fatal(err)
	}
	k := &exitLANBPFPinTestKernel{link: link, pins: map[string]int{}}
	p.call = k.call
	d := &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }, closeFD: func(int) error { return nil }}
	return p, d, k
}

func TestExitLANBPFPinsRetainExactObjectsAfterClose(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		p, d, k := newExitLANBPFPinFixture(t, order, api.ExitFamilyDualStack)
		deadline, clock := exitLANBPFTestDeadline(t)
		if err := publishExitLANBPFTestDeadline(p, t.Context(), deadline, clock); err != nil {
			t.Fatal(err)
		}
		if k.link.k.objects[p.outer].slot == 0 {
			t.Fatal("missing initial lease")
		}
		created, err := pinExitLANBPFTestClosed(p, t.Context(), d, exitLANBPFTestScope)
		if err != nil || len(created) != 4 || len(k.pins) != 4 || k.link.k.objects[p.outer].slot != 0 {
			t.Fatal("closed pin publication failed", err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.link.k.assertClosed()
		for _, name := range created {
			fd, err := p.pinCommand(exitLANBPFObjectGet, 800, 0, name)
			if err != nil {
				t.Fatal("pin did not retain object", err)
			}
			if err = p.closeFD(fd); err != nil {
				t.Fatal(err)
			}
		}
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		k.link.k.assertClosed()
	}
}

func TestExitLANBPFPinFailurePreservesClosedPartialOwnership(t *testing.T) {
	for step := 1; step <= 21; step++ {
		for _, lateCancel := range []bool{false, true} {
			p, d, k := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
			ctx, cancel := context.WithCancel(t.Context())
			if lateCancel {
				k.after = func(int) {
					if k.steps == step {
						cancel()
					}
				}
			} else {
				k.fail = step
			}
			created, err := pinExitLANBPFTestClosed(p, ctx, d, exitLANBPFTestScope)
			cancel()
			if err == nil || (lateCancel && !errors.Is(err, context.Canceled)) {
				t.Fatal("pin failure not propagated", step, lateCancel, err)
			}
			if len(k.pins) != len(created) || k.link.k.objects[p.outer].slot != 0 {
				t.Fatal("partial pins replaced/removed or lease opened", step)
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			k.link.k.assertClosed()
		}
	}
}

func TestExitLANBPFPinConflictAndReadbackMismatch(t *testing.T) {
	for _, scenario := range []string{"existing", "swapped", "same_type", "directory_lost"} {
		p, d, k := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
		names, _ := exitLANBPFPinNames(exitLANBPFTestScope)
		switch scenario {
		case "existing":
			k.pins[names[3]] = p.program
		case "swapped":
			k.after = func(command int) {
				if command == exitLANBPFObjectPin && len(k.pins) == 2 {
					k.pins[names[1]] = p.outer
				}
			}
		case "same_type":
			original := p.call
			p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
				fd, err := original(command, attr, buffers...)
				if err == nil && command == exitLANBPFInfo && len(buffers) == 1 && len(buffers[0]) == 8 && p.order.Uint32(buffers[0]) == exitLANBPFArrayOfMaps && int(p.order.Uint32(attr)) != p.outer {
					p.order.PutUint32(buffers[0][4:], 201)
				}
				return fd, err
			}
		case "directory_lost":
			checks := 0
			d.check = func(int) error {
				checks++
				if checks > 2 {
					return errExitLANBPF
				}
				return nil
			}
		}
		_, err := pinExitLANBPFTestClosed(p, t.Context(), d, exitLANBPFTestScope)
		if err == nil {
			t.Fatal("invalid pins accepted", scenario)
		}
		if scenario == "existing" && (len(k.pins) != 1 || k.pins[names[3]] != p.program) {
			t.Fatal("foreign pin was changed")
		}
		if k.link.k.objects[p.outer].slot != 0 {
			t.Fatal("failure opened lease")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.link.k.assertClosed()
	}
}

func TestExitLANBPFPinsRejectUnsafeNamesAndDirectory(t *testing.T) {
	p, d, k := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
	defer func() { _ = p.Close() }()
	for _, scope := range []string{"", "../" + exitLANBPFTestScope, strings.ToUpper(exitLANBPFTestScope), strings.Repeat("g", 24)} {
		if _, err := pinExitLANBPFTestClosed(p, t.Context(), d, scope); err == nil || k.steps != 0 {
			t.Fatal("unsafe scope reached kernel")
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "/absolute", "a\x00b", strings.Repeat("a", 65)} {
		if _, err := p.pinCommand(exitLANBPFObjectGet, 800, 0, name); err == nil || k.steps != 0 {
			t.Fatal("unsafe name reached kernel")
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pinExitLANBPFTestClosed(p, t.Context(), d, exitLANBPFTestScope); err == nil || k.steps != 0 {
		t.Fatal("closed directory reached kernel")
	}
}

func TestExitLANBPFPinsFollowFamilyIdentity(t *testing.T) {
	for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only} {
		p, d, k := newExitLANBPFPinFixture(t, binary.LittleEndian, mode)
		created, err := pinExitLANBPFTestClosed(p, t.Context(), d, exitLANBPFTestScope)
		if err != nil || len(created) != 3 || len(k.pins) != 3 {
			t.Fatal("single-family pins rejected", mode, err)
		}
		suffix, absent := "_ipv4", "_ipv6"
		if mode == api.ExitFamilyIPv6Only {
			suffix, absent = absent, suffix
		}
		if created[2] != exitLANBPFTestScope+suffix || k.pins[created[2]] != p.links[0].fd {
			t.Fatal("link name used slice position instead of family")
		}
		if _, exists := k.pins[exitLANBPFTestScope+absent]; exists {
			t.Fatal("unselected family was pinned")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.link.k.assertClosed()
	}
	for _, scenario := range []string{"wrong_family", "missing_link", "duplicate_link", "unknown_mode", "foreign_unselected_pin"} {
		p, d, k := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyIPv6Only)
		saved := append([]exitLANBPFLink(nil), p.links...)
		switch scenario {
		case "wrong_family":
			p.links[0].identity.family = 2
		case "missing_link":
			p.links = nil
		case "duplicate_link":
			p.links = append(p.links, p.links[0])
		case "unknown_mode":
			p.family = ""
		case "foreign_unselected_pin":
			k.pins[exitLANBPFTestScope+"_ipv4"] = p.program
		}
		created, err := pinExitLANBPFTestClosed(p, t.Context(), d, exitLANBPFTestScope)
		if err == nil || len(created) != 0 {
			t.Fatal("unbound family pins accepted", scenario)
		}
		if scenario == "foreign_unselected_pin" && (len(k.pins) != 1 || k.pins[exitLANBPFTestScope+"_ipv4"] != p.program) {
			t.Fatal("old family pin changed")
		}
		p.links = saved
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		k.link.k.assertClosed()
	}
}
