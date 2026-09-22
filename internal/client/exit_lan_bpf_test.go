package client

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

type exitLANBPFTestObject struct {
	kind, flags    uint32
	frozen, closed bool
	value          uint64
	slot           int
}
type exitLANBPFTestKernel struct {
	t                   *testing.T
	order               binary.ByteOrder
	objects             map[int]*exitLANBPFTestObject
	next, calls, failAt int
	after               func(int, int) error
}

func newExitLANBPFTestKernel(t *testing.T, order binary.ByteOrder) *exitLANBPFTestKernel {
	return &exitLANBPFTestKernel{t: t, order: order, objects: map[int]*exitLANBPFTestObject{}, next: 10}
}
func (k *exitLANBPFTestKernel) call(cmd int, attr []byte, buffers ...[]byte) (int, error) {
	k.calls++
	if k.calls == k.failAt {
		return -1, errExitLANBPF
	}
	fd := 0
	u32 := func(at int) uint32 { return k.order.Uint32(attr[at:]) }
	ptr := func(at, index int) {
		if k.order.Uint64(attr[at:]) != exitLANBPFPointer(buffers[index]) {
			k.t.Fatal("incorrect UAPI buffer pointer")
		}
	}
	switch cmd {
	case exitLANBPFMapCreate:
		if len(attr) != 72 || u32(4) != 4 || u32(12) != 1 {
			k.t.Fatal("incorrect map layout")
		}
		kind, value, flags := u32(0), u32(8), u32(16)
		switch kind {
		case exitLANBPFArray:
			if value != 8 || flags != exitLANBPFReadOnlyProgram {
				k.t.Fatal("mutable/incorrect inner map")
			}
		case exitLANBPFArrayOfMaps:
			template := k.objects[int(u32(20))]
			if value != 4 || flags != 0 || template == nil || template.closed || !template.frozen || template.kind != exitLANBPFArray {
				k.t.Fatal("invalid outer/template")
			}
		default:
			k.t.Fatal("unexpected map kind")
		}
		fd = k.next
		k.next++
		k.objects[fd] = &exitLANBPFTestObject{kind: kind, flags: flags}
	case exitLANBPFMapFreeze:
		if len(attr) != 4 {
			k.t.Fatal("incorrect FREEZE layout")
		}
		obj := k.objects[int(u32(0))]
		if obj == nil || obj.closed || obj.kind != exitLANBPFArray {
			k.t.Fatal("invalid freeze")
		}
		obj.frozen = true
	case exitLANBPFMapUpdate:
		if len(attr) != 32 || len(buffers) != 2 || len(buffers[0]) != 4 || k.order.Uint32(buffers[0]) != 0 || k.order.Uint64(attr[24:]) != 0 {
			k.t.Fatal("incorrect UPDATE layout")
		}
		ptr(8, 0)
		ptr(16, 1)
		obj := k.objects[int(u32(0))]
		if obj == nil || obj.closed || obj.frozen {
			k.t.Fatal("update of invalid/frozen object")
		}
		if obj.kind == exitLANBPFArray {
			if len(buffers[1]) != 8 {
				k.t.Fatal("wrong lease size")
			}
			obj.value = k.order.Uint64(buffers[1])
		} else {
			if len(buffers[1]) != 4 {
				k.t.Fatal("outer value must be u32 FD")
			}
			inner := int(k.order.Uint32(buffers[1]))
			candidate := k.objects[inner]
			if candidate == nil || candidate.closed || !candidate.frozen || candidate.flags != exitLANBPFReadOnlyProgram {
				k.t.Fatal("nonimmutable lease publication")
			}
			obj.slot = inner
		}
	case exitLANBPFMapDelete:
		if len(attr) != 16 || len(buffers) != 1 || len(buffers[0]) != 4 || k.order.Uint32(buffers[0]) != 0 {
			k.t.Fatal("incorrect DELETE layout")
		}
		ptr(8, 0)
		obj := k.objects[int(u32(0))]
		if obj == nil || obj.closed || obj.kind != exitLANBPFArrayOfMaps {
			k.t.Fatal("invalid revocation")
		}
		obj.slot = 0
	case exitLANBPFProgLoad:
		if len(attr) != 72 || len(buffers) != 3 || u32(0) != 32 || u32(68) != 45 || int(u32(4))*8 != len(buffers[0]) || string(buffers[1]) != "GPL\x00" || u32(24) != 1 || u32(28) != uint32(len(buffers[2])) {
			k.t.Fatal("incorrect program LOAD layout")
		}
		if string(attr[48:64]) != "en_lan_gate\x00\x00\x00\x00\x00" || u32(40) != 0 || u32(44) != 0 || u32(64) != 0 {
			k.t.Fatal("incorrect program name or unexpected load options")
		}
		ptr(8, 0)
		ptr(16, 1)
		ptr(32, 2)
		fd = k.next
		k.next++
		k.objects[fd] = &exitLANBPFTestObject{kind: 999}
	default:
		k.t.Fatal("unexpected UAPI command", cmd)
	}
	if k.after != nil {
		if err := k.after(cmd, fd); err != nil {
			return -1, err
		}
	}
	return fd, nil
}
func (k *exitLANBPFTestKernel) close(fd int) error {
	obj := k.objects[fd]
	if obj == nil || obj.closed {
		k.t.Fatal("foreign or double close", fd)
	}
	obj.closed = true
	return nil
}
func (k *exitLANBPFTestKernel) assertClosed() {
	for fd, obj := range k.objects {
		if !obj.closed {
			k.t.Fatal("leaked FD", fd)
		}
	}
}
func exitLANBPFTestDeadline(t *testing.T) (*exitLANBootDeadline, func(context.Context) (exitLANClockSample, error)) {
	t.Helper()
	now := time.Now().UTC()
	d, err := newExitLANBootDeadline(exitLANClockSample{bootBefore: 1e9, bootAfter: 1e9 + 1, wall: now}, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return d, func(context.Context) (exitLANClockSample, error) {
		return exitLANClockSample{bootBefore: 2e9, bootAfter: 2e9 + 1, wall: now.Add(time.Second)}, nil
	}
}

// Low-level map tests isolate immutable publication from attachment checks.
// Production publication must use publishBootDeadline's required live observer.
func publishExitLANBPFTestDeadline(p *exitLANBPFPreparation, ctx context.Context, deadline *exitLANBootDeadline, clock func(context.Context) (exitLANClockSample, error)) error {
	if !p.mu.TryLock() && !lockExitRuntime(ctx, &p.mu) {
		return ctx.Err()
	}
	defer p.mu.Unlock()
	return p.publishBootDeadlineLocked(ctx, deadline, clock, func() error { return nil })
}

func TestExitLANBPFClosedPreparationAndImmutablePublication(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		k := newExitLANBPFTestKernel(t, order)
		p, err := newExitLANBPFPreparation(t.Context(), 0x80000001, 8, 64, 32, 45, order, k.call, k.close)
		if err != nil {
			t.Fatal(err)
		}
		if k.objects[p.outer].slot != 0 || !k.objects[10].closed {
			k.t.Fatal("initial preparation not closed or template leaked")
		}
		d, clock := exitLANBPFTestDeadline(t)
		if err = publishExitLANBPFTestDeadline(p, t.Context(), d, clock); err != nil {
			t.Fatal(err)
		}
		inner := k.objects[k.objects[p.outer].slot]
		if inner == nil || !inner.frozen || !inner.closed || inner.value != d.bootExpires {
			k.t.Fatal("publication did not retain exact immutable expiry")
		}
		if err = p.revoke(); err != nil || k.objects[p.outer].slot != 0 {
			t.Fatal("revoke retained lease", err)
		}
		if err = p.revoke(); err != nil {
			t.Fatal("revoke was not idempotent", err)
		}
		if err = p.Close(); err != nil {
			t.Fatal(err)
		}
		if err = p.Close(); err != nil {
			t.Fatal(err)
		}
		k.assertClosed()
		if err = publishExitLANBPFTestDeadline(p, t.Context(), d, clock); err == nil {
			t.Fatal("closed objects republished")
		}
	}
}

func TestExitLANBPFPreparationFailureClosesOnlyOwnedFDs(t *testing.T) {
	for fail := 1; fail <= 4; fail++ {
		k := newExitLANBPFTestKernel(t, binary.LittleEndian)
		k.failAt = fail
		if p, err := newExitLANBPFPreparation(t.Context(), 7, 8, 64, 32, 45, binary.LittleEndian, k.call, k.close); err == nil || p != nil {
			t.Fatal("failed creation returned object", fail)
		}
		k.assertClosed()
	}
	for _, cmd := range []int{exitLANBPFMapCreate, exitLANBPFMapFreeze, exitLANBPFProgLoad} {
		ctx, cancel := context.WithCancel(t.Context())
		k := newExitLANBPFTestKernel(t, binary.LittleEndian)
		k.after = func(command, fd int) error {
			if command == cmd {
				cancel()
			}
			return nil
		}
		p, err := newExitLANBPFPreparation(ctx, 7, 8, 64, 32, 45, binary.LittleEndian, k.call, k.close)
		cancel()
		if !errors.Is(err, context.Canceled) || p != nil {
			t.Fatal("late create cancellation ignored", cmd, err)
		}
		k.assertClosed()
	}
}

func TestExitLANBPFPublicationFailureAndLateInvalidation(t *testing.T) {
	for _, scenario := range []string{"create", "write", "freeze", "publish", "late_cancel", "late_expiry"} {
		t.Run(scenario, func(t *testing.T) {
			k := newExitLANBPFTestKernel(t, binary.LittleEndian)
			p, err := newExitLANBPFPreparation(t.Context(), 7, 8, 64, 32, 45, binary.LittleEndian, k.call, k.close)
			if err != nil {
				t.Fatal(err)
			}
			d, clock := exitLANBPFTestDeadline(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := k.calls
			switch scenario {
			case "create":
				k.failAt = base + 1
			case "write":
				k.failAt = base + 2
			case "freeze":
				k.failAt = base + 3
			}
			published := false
			k.after = func(cmd, fd int) error {
				if cmd == exitLANBPFMapUpdate && k.objects[p.outer].slot != 0 {
					published = true
					if scenario == "publish" {
						return errExitLANBPF
					}
					if scenario == "late_cancel" {
						cancel()
					}
				}
				return nil
			}
			read := func(ctx context.Context) (exitLANClockSample, error) {
				sample, err := clock(ctx)
				if scenario == "late_expiry" && published {
					sample.bootBefore = d.bootExpires
					sample.bootAfter = d.bootExpires + 1
				}
				return sample, err
			}
			if err = publishExitLANBPFTestDeadline(p, ctx, d, read); err == nil {
				t.Fatal("invalid publication succeeded")
			}
			if k.objects[p.outer].slot != 0 {
				t.Fatal("ambiguous publication not revoked")
			}
			if scenario == "late_cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if err = p.Close(); err != nil {
				t.Fatal(err)
			}
			k.assertClosed()
		})
	}
}

func TestExitLANBPFFailedRefreshRevokesPreviousLease(t *testing.T) {
	for _, scenario := range []string{"cancelled", "clock", "expired", "nil_clock", "nil_deadline", "create", "write", "freeze", "publish", "revoke_failure"} {
		t.Run(scenario, func(t *testing.T) {
			k := newExitLANBPFTestKernel(t, binary.LittleEndian)
			p, err := newExitLANBPFPreparation(t.Context(), 7, 8, 64, 32, 45, binary.LittleEndian, k.call, k.close)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Close() }()
			d, clock := exitLANBPFTestDeadline(t)
			if err := publishExitLANBPFTestDeadline(p, t.Context(), d, clock); err != nil {
				t.Fatal(err)
			}
			previous := k.objects[p.outer].slot
			if previous == 0 {
				t.Fatal("missing initial lease")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			revokeFailure := errors.New("synthetic revocation failure")
			switch scenario {
			case "cancelled":
				cancel()
			case "clock":
				clock = func(context.Context) (exitLANClockSample, error) { return exitLANClockSample{}, errExitLANClock }
			case "expired":
				d.bootExpires = 1
			case "nil_clock":
				clock = nil
			case "nil_deadline":
				d = nil
			case "create":
				k.failAt = k.calls + 1
			case "write":
				k.failAt = k.calls + 2
			case "freeze":
				k.failAt = k.calls + 3
			case "publish":
				k.failAt = k.calls + 4
			case "revoke_failure":
				cancel()
				original := p.call
				p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
					if command == exitLANBPFMapDelete {
						return -1, revokeFailure
					}
					return original(command, attr, buffers...)
				}
			}
			err = publishExitLANBPFTestDeadline(p, ctx, d, clock)
			if err == nil {
				t.Fatal("invalid refresh succeeded")
			}
			if scenario == "revoke_failure" {
				if !errors.Is(err, revokeFailure) || !errors.Is(err, context.Canceled) || k.objects[p.outer].slot != previous {
					t.Fatal("ambiguous revocation did not preserve both failure causes")
				}
			} else if k.objects[p.outer].slot != 0 {
				t.Fatal("failed refresh retained previous lease")
			}
		})
	}
}

func TestExitLANBPFPublicationCancellationDuringContention(t *testing.T) {
	k := newExitLANBPFTestKernel(t, binary.LittleEndian)
	p, err := newExitLANBPFPreparation(t.Context(), 7, 8, 64, 32, 45, binary.LittleEndian, k.call, k.close)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	d, clock := exitLANBPFTestDeadline(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p.mu.Lock()
	before := k.calls
	done := make(chan error, 1)
	go func() { done <- publishExitLANBPFTestDeadline(p, ctx, d, clock) }()
	cancel()
	select {
	case err := <-done:
		p.mu.Unlock()
		if !errors.Is(err, context.Canceled) || k.calls != before {
			t.Fatal("contended cancellation touched map or lost context", err)
		}
	case <-time.After(time.Second):
		p.mu.Unlock()
		<-done
		t.Fatal("cancelled publisher waited for map ownership")
	}
}
