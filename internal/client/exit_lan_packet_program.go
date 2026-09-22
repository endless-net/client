package client

import (
	"encoding/binary"
	"math"
)

type exitLANPacketDevice struct {
	index    uint32
	instance uint64
}

// The postrouting gate intersects the lease with the final output device and
// route. nft supplies exact source/destination-prefix matching. A reused name
// or ifindex cannot match a different kernfs ID. Probe failures always DROP.
func buildExitLANPacketProgram(layout exitLANPacketLayout, devices []exitLANPacketDevice, mark uint32, outerFD int, order binary.ByteOrder) ([]byte, error) {
	if (order != binary.LittleEndian && order != binary.BigEndian) || mark == 0 || outerFD < 0 || outerFD > math.MaxInt32 || len(devices) == 0 || len(devices) > 128 {
		return nil, errExitLANBPFProgram
	}
	for _, offset := range []uint32{layout.ctxSKB, layout.ctxState, layout.skbMark, layout.skbDst, layout.stateOut, layout.stateFamily, layout.deviceIndex, layout.deviceSD, layout.kernfsID, layout.dstDevice, layout.ipv4Gateway, layout.ipv4Type, layout.ipv6Flags} {
		if offset > math.MaxInt16 {
			return nil, errExitLANBPFProgram
		}
	}
	seen := map[uint32]bool{}
	for _, d := range devices {
		if d.index == 0 || d.index > math.MaxInt32 || d.instance == 0 || seen[d.index] {
			return nil, errExitLANBPFProgram
		}
		seen[d.index] = true
	}
	code := []byte{}
	labels := map[string]int{}
	type fixup struct {
		at    int
		label string
	}
	fixups := []fixup{}
	emit := func(op, dst, src byte, offset int16, imm uint32) {
		var row [8]byte
		row[0] = op
		row[1] = src<<4 | dst
		if order == binary.BigEndian {
			row[1] = dst<<4 | src
		}
		order.PutUint16(row[2:], uint16(offset))
		order.PutUint32(row[4:], imm)
		code = append(code, row[:]...)
	}
	jump := func(op, dst, src byte, imm uint32, label string) {
		fixups = append(fixups, fixup{len(code) / 8, label})
		emit(op, dst, src, 0, imm)
	}
	label := func(name string) { labels[name] = len(code) / 8 }
	wide := func(dst byte, value uint64) {
		emit(0x18, dst, 0, 0, uint32(value))
		emit(0, 0, 0, 0, uint32(value>>32))
	}
	// Read into zeroed stack memory. Untrusted scalar pointers (including
	// skb_dst's low-bit encoding) are never dereferenced with a direct LDX.
	read := func(base, dst byte, offset uint32, size uint32) {
		emit(0x7a, 10, 0, -32, 0)
		emit(0xbf, 3, base, 0, 0)
		emit(0x07, 3, 0, 0, offset)
		emit(0xbf, 1, 10, 0, 0)
		emit(0x07, 1, 0, 0, uint32(0xffffffe0))
		emit(0xb7, 2, 0, 0, size)
		emit(0x85, 0, 0, 0, 113)
		jump(0x55, 0, 0, 0, "drop")
		op := byte(0x79)
		if size == 1 {
			op = 0x71
		}
		if size == 2 {
			op = 0x69
		}
		if size == 4 {
			op = 0x61
		}
		emit(op, dst, 10, -32, 0)
	}
	emit(0xbf, 9, 1, 0, 0)
	emit(0x79, 6, 1, int16(layout.ctxSKB), 0)
	emit(0x7b, 10, 6, -16, 0)
	emit(0x61, 2, 6, int16(layout.skbMark), 0)
	emit(0xb4, 3, 0, 0, mark)
	jump(0x5d, 2, 3, 0, "accept")
	emit(0x62, 10, 0, -4, 0)
	emit(0x18, 1, 1, 0, uint32(outerFD))
	emit(0, 0, 0, 0, 0)
	emit(0xbf, 2, 10, 0, 0)
	emit(0x07, 2, 0, 0, 0xfffffffc)
	emit(0x85, 0, 0, 0, 1)
	jump(0x15, 0, 0, 0, "drop")
	emit(0xbf, 1, 0, 0, 0)
	emit(0xbf, 2, 10, 0, 0)
	emit(0x07, 2, 0, 0, 0xfffffffc)
	emit(0x85, 0, 0, 0, 1)
	jump(0x15, 0, 0, 0, "drop")
	emit(0x79, 8, 0, 0, 0)
	emit(0x85, 0, 0, 0, 125)
	jump(0x3d, 0, 8, 0, "drop")
	emit(0x79, 6, 9, int16(layout.ctxState), 0)
	emit(0x71, 9, 6, int16(layout.stateFamily), 0)
	read(6, 7, layout.stateOut, 8)
	jump(0x15, 7, 0, 0, "drop")
	read(7, 8, layout.deviceSD, 8)
	jump(0x15, 8, 0, 0, "drop")
	read(8, 8, layout.kernfsID, 8)
	read(7, 6, layout.deviceIndex, 4)
	for _, d := range devices {
		// Skip the two-instruction constant and its compare when the index is
		// different. Both instance and index must refer to the same binding.
		emit(0x55, 6, 0, 3, d.index)
		wide(2, d.instance)
		jump(0x1d, 8, 2, 0, "route")
	}
	jump(0x05, 0, 0, 0, "drop")
	label("route")
	emit(0x79, 6, 10, -16, 0)
	emit(0x79, 8, 6, int16(layout.skbDst), 0)
	emit(0x57, 8, 0, 0, 0xfffffffe)
	jump(0x15, 8, 0, 0, "drop")
	read(8, 6, layout.dstDevice, 8)
	jump(0x5d, 6, 7, 0, "drop")
	jump(0x15, 9, 0, 10, "ipv6")
	jump(0x55, 9, 0, 2, "drop")
	read(8, 6, layout.ipv4Gateway, 1)
	jump(0x55, 6, 0, 0, "drop")
	read(8, 6, layout.ipv4Type, 2)
	jump(0x55, 6, 0, 1, "drop") // RTN_UNICAST only
	jump(0x05, 0, 0, 0, "accept")
	label("ipv6")
	read(8, 6, layout.ipv6Flags, 4)
	emit(0x57, 6, 0, 0, 0x2) // RTF_GATEWAY
	jump(0x55, 6, 0, 0, "drop")
	label("accept")
	emit(0xb7, 0, 0, 0, 1)
	emit(0x95, 0, 0, 0, 0)
	label("drop")
	emit(0xb7, 0, 0, 0, 0)
	emit(0x95, 0, 0, 0, 0)
	for _, fix := range fixups {
		at, ok := labels[fix.label]
		distance := at - fix.at - 1
		if !ok || distance < 0 || distance > math.MaxInt16 {
			return nil, errExitLANBPFProgram
		}
		order.PutUint16(code[fix.at*8+2:], uint16(distance))
	}
	return code, nil
}
