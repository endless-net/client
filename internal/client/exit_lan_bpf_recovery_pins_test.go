package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANRecoveryPinObject struct {
	kind, id uint32
	link     exitLANBPFLinkIdentity
	revokes  int
}
type exitLANRecoveryPinKernel struct {
	t                               *testing.T
	order                           binary.ByteOrder
	pins                            map[string]*exitLANRecoveryPinObject
	fds                             map[int]*exitLANRecoveryPinObject
	closed                          map[int]bool
	next, calls, gets, writes, fail int
	after                           func(int)
}

func newExitLANRecoveryPinKernel(t *testing.T, order binary.ByteOrder, o *exitLANOwnership, mask int) *exitLANRecoveryPinKernel {
	k := &exitLANRecoveryPinKernel{t: t, order: order, pins: map[string]*exitLANRecoveryPinObject{}, fds: map[int]*exitLANRecoveryPinObject{}, closed: map[int]bool{}, next: 10}
	names, _ := exitLANBPFPinNames(o.Scope)
	objects := [4]*exitLANRecoveryPinObject{{kind: exitLANBPFArrayOfMaps, id: o.MapID}, {kind: 32, id: o.ProgramID}}
	for _, link := range o.Links {
		i := 2
		if link.Family == 10 {
			i = 3
		}
		objects[i] = &exitLANRecoveryPinObject{kind: exitLANBPFNetfilterLink, id: link.ID, link: exitLANBPFLinkIdentity{link.ID, link.ProgramID, link.Family, link.Hook, link.Priority}}
	}
	for i, obj := range objects {
		if obj != nil && mask&(1<<i) != 0 {
			k.pins[names[i]] = obj
		}
	}
	return k
}
func (k *exitLANRecoveryPinKernel) call(command int, attr []byte, buffers ...[]byte) (int, error) {
	k.calls++
	if k.calls == k.fail {
		return -1, errExitLANBPF
	}
	defer func() {
		if k.after != nil {
			k.after(command)
		}
	}()
	switch command {
	case exitLANBPFObjectGet:
		k.gets++
		if len(attr) != 20 || len(buffers) != 1 || k.order.Uint32(attr[12:]) != exitLANBPFPathFD || k.order.Uint32(attr[16:]) != 800 {
			k.t.Fatal("invalid relative GET")
		}
		obj := k.pins[string(buffers[0][:len(buffers[0])-1])]
		if obj == nil {
			return -1, fs.ErrNotExist
		}
		fd := k.next
		k.next++
		k.fds[fd] = obj
		return fd, nil
	case exitLANBPFInfo:
		fd := int(k.order.Uint32(attr))
		obj := k.fds[fd]
		if obj == nil || k.closed[fd] {
			k.t.Fatal("INFO without held fd")
		}
		info := buffers[0]
		k.order.PutUint32(info, obj.kind)
		k.order.PutUint32(info[4:], obj.id)
		if len(info) == 32 {
			for i, v := range []uint32{obj.link.programID, 0, obj.link.family, obj.link.hook, uint32(obj.link.priority), 0} {
				k.order.PutUint32(info[8+i*4:], v)
			}
		}
		return 0, nil
	case exitLANBPFMapDelete:
		fd := int(k.order.Uint32(attr))
		obj := k.fds[fd]
		if obj == nil || k.closed[fd] || obj.kind != exitLANBPFArrayOfMaps {
			k.t.Fatal("foreign revoke")
		}
		k.writes++
		obj.revokes++
		return 0, nil
	default:
		k.t.Fatal("unexpected recovery mutation", command)
	}
	return -1, errExitLANBPF
}
func (k *exitLANRecoveryPinKernel) close(fd int) error {
	if k.fds[fd] == nil || k.closed[fd] {
		k.t.Fatal("foreign/double close", fd)
	}
	k.closed[fd] = true
	return nil
}
func (k *exitLANRecoveryPinKernel) assertClosed() {
	k.t.Helper()
	for fd := range k.fds {
		if !k.closed[fd] {
			k.t.Fatal("leaked recovered fd", fd)
		}
	}
}
func exitLANRecoveryTestDirectory() *exitLANBPFDirectory {
	return &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }}
}

func TestExitLANRecoveryPinsAllPartialSubsets(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
			for mask := 0; mask < 16; mask++ {
				o := exitLANOwnershipFixture(mode)
				k := newExitLANRecoveryPinKernel(t, order, o, mask)
				names, _ := exitLANBPFPinNames(o.Scope)
				count := len(k.pins)
				p, err := openExitLANBPFOwnedPins(t.Context(), exitLANRecoveryTestDirectory(), o, order, k.call, k.close)
				if err != nil || p == nil {
					t.Fatal("partial inventory failed", mode, mask, err)
				}
				for i, name := range names {
					if (p.fds[i] >= 0) != (k.pins[name] != nil) {
						t.Fatal("incorrect presence", i)
					}
				}
				mapPresent := k.pins[names[0]] != nil
				if p.leaseRevoked != mapPresent || k.writes != boolExitLANRecoveryInt(mapPresent) || k.gets != 8 {
					t.Fatal("incorrect revocation or missing second snapshot")
				}
				o.Links[0].ID++
				if p.ownership.Links[0].ID == o.Links[0].ID {
					t.Fatal("ownership aliases caller")
				}
				if err := p.Close(); err != nil {
					t.Fatal(err)
				}
				if err := p.Close(); err != nil {
					t.Fatal(err)
				}
				k.assertClosed()
				if len(k.pins) != count {
					t.Fatal("recovery removed pins")
				}
			}
		}
	}
}
func boolExitLANRecoveryInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestExitLANRecoveryPinsForeignOrChangingNeverMutates(t *testing.T) {
	for _, scenario := range []string{"map_id", "map_type", "program_id", "link_id", "link_program", "link_hook", "link_priority", "link_family", "unselected", "disappears", "appears", "rebound", "directory"} {
		t.Run(scenario, func(t *testing.T) {
			o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
			if scenario == "unselected" {
				o = exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
			}
			mask := 15
			if scenario == "appears" {
				mask = 14
			}
			k := newExitLANRecoveryPinKernel(t, binary.LittleEndian, o, mask)
			names, _ := exitLANBPFPinNames(o.Scope)
			switch scenario {
			case "map_id":
				k.pins[names[0]].id++
			case "map_type":
				k.pins[names[0]].kind = 32
			case "program_id":
				k.pins[names[1]].id++
			case "link_id":
				k.pins[names[2]].id++
			case "link_program":
				k.pins[names[2]].link.programID++
			case "link_hook":
				k.pins[names[2]].link.hook--
			case "link_priority":
				k.pins[names[2]].link.priority++
			case "link_family":
				k.pins[names[2]].link.family = 10
			case "unselected":
				k.pins[names[3]] = &exitLANRecoveryPinObject{kind: 10, id: 999}
			}
			if scenario == "disappears" || scenario == "appears" || scenario == "rebound" {
				k.after = func(command int) {
					if command == exitLANBPFObjectGet && k.gets == 4 {
						switch scenario {
						case "disappears":
							delete(k.pins, names[0])
						case "appears":
							k.pins[names[0]] = &exitLANRecoveryPinObject{kind: exitLANBPFArrayOfMaps, id: o.MapID}
						case "rebound":
							k.pins[names[0]] = &exitLANRecoveryPinObject{kind: exitLANBPFArrayOfMaps, id: o.MapID + 1}
						}
					}
				}
			}
			directory := exitLANRecoveryTestDirectory()
			if scenario == "directory" {
				checks := 0
				directory.check = func(int) error {
					checks++
					if checks == 2 {
						return errExitLANBPF
					}
					return nil
				}
			}
			p, err := openExitLANBPFOwnedPins(t.Context(), directory, o, binary.LittleEndian, k.call, k.close)
			if p != nil || err == nil || k.writes != 0 {
				t.Fatal("foreign or changing pins reached mutation", scenario, err)
			}
			k.assertClosed()
		})
	}
}

func TestExitLANRecoveryPinsCancellationFailureAndRevokeError(t *testing.T) {
	for _, scenario := range []string{"pre_cancel", "late_get", "after_revoke", "revoke_error", "get_error", "metadata_error"} {
		t.Run(scenario, func(t *testing.T) {
			o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
			k := newExitLANRecoveryPinKernel(t, binary.LittleEndian, o, 15)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "pre_cancel" {
				cancel()
			}
			if scenario == "get_error" {
				k.fail = 1
			}
			if scenario == "metadata_error" {
				k.fail = 2
			}
			call := func(command int, attr []byte, buffers ...[]byte) (int, error) {
				if scenario == "revoke_error" && command == exitLANBPFMapDelete {
					return -1, errExitLANBPF
				}
				fd, err := k.call(command, attr, buffers...)
				if scenario == "late_get" && command == exitLANBPFObjectGet || scenario == "after_revoke" && command == exitLANBPFMapDelete {
					cancel()
				}
				return fd, err
			}
			p, err := openExitLANBPFOwnedPins(ctx, exitLANRecoveryTestDirectory(), o, binary.LittleEndian, call, k.close)
			if p != nil || err == nil {
				t.Fatal("failed recovery returned inventory")
			}
			if scenario == "pre_cancel" || scenario == "late_get" || scenario == "after_revoke" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation", err)
				}
			}
			if scenario == "pre_cancel" && k.calls != 0 {
				t.Fatal("pre-canceled recovery touched kernel")
			}
			if scenario != "after_revoke" && k.writes != 0 {
				t.Fatal("failure mutated map")
			}
			k.assertClosed()
		})
	}
}

func TestExitLANRecoveryPinsCloseErrorPreventsMutation(t *testing.T) {
	o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
	k := newExitLANRecoveryPinKernel(t, binary.LittleEndian, o, 15)
	closeFailure := errors.New("synthetic descriptor close failure")
	first := true
	closeFD := func(fd int) error {
		err := k.close(fd)
		if first {
			first = false
			return errors.Join(err, closeFailure)
		}
		return err
	}
	p, err := openExitLANBPFOwnedPins(t.Context(), exitLANRecoveryTestDirectory(), o, binary.LittleEndian, k.call, closeFD)
	if p != nil || !errors.Is(err, closeFailure) || k.writes != 0 {
		t.Fatal("uncertain second snapshot permitted mutation", err)
	}
	k.assertClosed()
}

func TestExitLANRecoveryPinsContendedDirectoryCancellation(t *testing.T) {
	o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
	k := newExitLANRecoveryPinKernel(t, binary.LittleEndian, o, 15)
	d := exitLANRecoveryTestDirectory()
	d.mu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		p, err := openExitLANBPFOwnedPins(ctx, d, o, binary.LittleEndian, k.call, k.close)
		if p != nil {
			_ = p.Close()
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		d.mu.Unlock()
		if !errors.Is(err, context.Canceled) || k.calls != 0 {
			t.Fatal("contended cancellation touched pins", err)
		}
	case <-time.After(time.Second):
		d.mu.Unlock()
		<-done
		t.Fatal("directory lock ignored cancellation")
	}
	k.assertClosed()
}
