package client

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"runtime"
	"sync"
	"unsafe"
)

var errExitLANBPF = errors.New("LAN BPF preparation is unavailable")

// Stable Linux UAPI commands; the Linux unit asserts these against pinned x/sys.
const (
	exitLANBPFMapCreate = 0
	exitLANBPFMapUpdate = 2
	exitLANBPFMapDelete = 3
	exitLANBPFProgLoad  = 5
	exitLANBPFMapFreeze = 22
	// ARRAY_OF_MAPS rejects RDONLY_PROG. Inner ARRAY must be both RDONLY_PROG
	// and frozen before publication; outer remains writable only by userspace.
	exitLANBPFArray           = 2
	exitLANBPFArrayOfMaps     = 12
	exitLANBPFReadOnlyProgram = 128
)

type exitLANBPFCall func(command int, attr []byte, buffers ...[]byte) (int, error)

// Loaded, initially closed, and UNATTACHED objects. This is not authority to
// open nft LAN exceptions: pinning, live hook readback, route ownership and
// cross-preparation evidence caps must precede that separate adapter step.
type exitLANBPFPreparation struct {
	mu             sync.Mutex
	outer, program int
	links          []exitLANBPFLink
	call           exitLANBPFCall
	closeFD        func(int) error
	order          binary.ByteOrder
}

func exitLANBPFPointer(buffer []byte) uint64 {
	if len(buffer) == 0 {
		return 0
	}
	return uint64(uintptr(unsafe.Pointer(&buffer[0])))
}

func (p *exitLANBPFPreparation) invoke(command int, attr []byte, buffers ...[]byte) (int, error) {
	result, err := p.call(command, attr, buffers...)
	runtime.KeepAlive(attr)
	runtime.KeepAlive(buffers)
	if err != nil {
		return -1, err
	}
	return result, nil
}

func (p *exitLANBPFPreparation) createMap(kind, valueSize, flags uint32, inner int) (int, error) {
	attr := make([]byte, 72)
	for i, value := range []uint32{kind, 4, valueSize, 1, flags, uint32(inner)} {
		p.order.PutUint32(attr[i*4:], value)
	}
	fd, err := p.invoke(exitLANBPFMapCreate, attr)
	if err != nil || fd < 0 || fd > math.MaxInt32 {
		return -1, errExitLANBPF
	}
	return fd, nil
}

func (p *exitLANBPFPreparation) freeze(fd int) error {
	attr := make([]byte, 4)
	p.order.PutUint32(attr, uint32(fd))
	_, err := p.invoke(exitLANBPFMapFreeze, attr)
	return err
}

func (p *exitLANBPFPreparation) update(fd int, value []byte) error {
	key := make([]byte, 4)
	attr := make([]byte, 32)
	p.order.PutUint32(attr, uint32(fd))
	p.order.PutUint64(attr[8:], exitLANBPFPointer(key))
	p.order.PutUint64(attr[16:], exitLANBPFPointer(value))
	_, err := p.invoke(exitLANBPFMapUpdate, attr, key, value)
	return err
}

func newExitLANBPFPreparation(ctx context.Context, mark, ctxOffset, markOffset, programType, attachType uint32, order binary.ByteOrder, call exitLANBPFCall, closeFD func(int) error) (*exitLANBPFPreparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if call == nil || closeFD == nil || unsafe.Sizeof(uintptr(0)) != 8 || (order != binary.LittleEndian && order != binary.BigEndian) || programType == 0 || attachType == 0 {
		return nil, errExitLANBPF
	}
	// Validate instruction parameters before creating any kernel object.
	if _, err := buildExitLANBPFProgram(ctxOffset, markOffset, mark, 0, order); err != nil {
		return nil, err
	}
	p := &exitLANBPFPreparation{outer: -1, program: -1, call: call, closeFD: closeFD, order: order}
	template := -1
	keep := false
	defer func() {
		if template >= 0 {
			_ = closeFD(template)
		}
		if !keep {
			_ = p.Close()
		}
	}()
	var err error
	template, err = p.createMap(exitLANBPFArray, 8, exitLANBPFReadOnlyProgram, 0)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = p.freeze(template); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	p.outer, err = p.createMap(exitLANBPFArrayOfMaps, 4, 0, template)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	program, err := buildExitLANBPFProgram(ctxOffset, markOffset, mark, p.outer, order)
	if err != nil {
		return nil, err
	}
	license := []byte("GPL\x00")
	log := make([]byte, 64<<10)
	attr := make([]byte, 72)
	order.PutUint32(attr, programType)
	order.PutUint32(attr[4:], uint32(len(program)/8))
	order.PutUint64(attr[8:], exitLANBPFPointer(program))
	order.PutUint64(attr[16:], exitLANBPFPointer(license))
	order.PutUint32(attr[24:], 1)
	order.PutUint32(attr[28:], uint32(len(log)))
	order.PutUint64(attr[32:], exitLANBPFPointer(log))
	copy(attr[48:64], "en_lan_gate")
	order.PutUint32(attr[68:], attachType)
	p.program, err = p.invoke(exitLANBPFProgLoad, attr, program, license, log)
	if err != nil || p.program < 0 || p.program > math.MaxInt32 {
		return nil, errExitLANBPF
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return p, nil
}

// Publication swaps one pointer to a frozen inner map; a packet can only see
// the complete old or complete new deadline. In-flight RCU readers may finish
// using the old lease. Revoke therefore does not replace nft containment.
func (p *exitLANBPFPreparation) publishBootDeadline(ctx context.Context, deadline *exitLANBootDeadline, clock func(context.Context) (exitLANClockSample, error)) (result error) {
	if p == nil {
		return errExitLANBPF
	}
	if !p.mu.TryLock() && !lockExitRuntime(ctx, &p.mu) {
		// The other publisher still owns the map. Caller must contain traffic
		// independently; an unacquired lock cannot promise lease revocation.
		return ctx.Err()
	}
	defer p.mu.Unlock()
	// A failed refresh cannot keep earlier authority alive. This includes
	// failures before the outer pointer swap, not just ambiguous publication.
	defer func() {
		if result != nil && p.outer >= 0 {
			result = errors.Join(result, p.revokeLocked())
		}
	}()
	if clock == nil || deadline == nil {
		return errExitLANBPF
	}
	if p.outer < 0 || p.program < 0 {
		return errExitLANBPF
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sample, err := clock(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !deadline.current(sample) {
			return errExitLANClock
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	inner, err := p.createMap(exitLANBPFArray, 8, exitLANBPFReadOnlyProgram, 0)
	if err != nil {
		return err
	}
	defer func() { _ = p.closeFD(inner) }()
	value := make([]byte, 8)
	p.order.PutUint64(value, deadline.bootExpires)
	if err = p.update(inner, value); err != nil {
		return err
	}
	if err = p.freeze(inner); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	fd := make([]byte, 4)
	p.order.PutUint32(fd, uint32(inner))
	// An ambiguous outer update must be revoked, including cancellation or
	// expiry discovered after the kernel accepted the update.
	if err = p.update(p.outer, fd); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	return nil
}

func (p *exitLANBPFPreparation) revokeLocked() error {
	if p.outer < 0 {
		return errExitLANBPF
	}
	key := make([]byte, 4)
	attr := make([]byte, 16)
	p.order.PutUint32(attr, uint32(p.outer))
	p.order.PutUint64(attr[8:], exitLANBPFPointer(key))
	_, err := p.invoke(exitLANBPFMapDelete, attr, key)
	return err
}

func (p *exitLANBPFPreparation) revoke() error {
	if p == nil {
		return errExitLANBPF
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revokeLocked()
}

// Close releases this object's FDs only. Future pinned attachments are not
// detached by this operation; removing them requires confirmed nft BLOCK.
func (p *exitLANBPFPreparation) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var err error
	for _, link := range p.links {
		err = errors.Join(err, p.closeFD(link.fd))
	}
	p.links = nil
	for _, fd := range []*int{&p.program, &p.outer} {
		if *fd >= 0 {
			err = errors.Join(err, p.closeFD(*fd))
			*fd = -1
		}
	}
	return err
}
