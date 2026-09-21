package client

import (
	"encoding/binary"
	"errors"
	"math"
)

var errExitLANBPFProgram = errors.New("LAN BPF program layout is unavailable")

// buildExitLANBPFProgram emits a deadline gate for a 64-bit target kernel. It
// does not load, attach or qualify a program. The caller must validate BTF and
// own a compatible ARRAY_OF_MAPS whose inner ARRAY has a u64 expiry at key zero.
// Published inner values must be immutable; no relative TTL is calculated here.
// NF_ACCEPT only continues netfilter traversal: it cannot override another
// hook's DROP and does not itself grant LAN topology or route authority.
func buildExitLANBPFProgram(ctxSKBOffset, skbMarkOffset, lanMark uint32, outerMapFD int, order binary.ByteOrder) ([]byte, error) {
	if order != binary.LittleEndian && order != binary.BigEndian || ctxSKBOffset > math.MaxInt16 || ctxSKBOffset%8 != 0 || skbMarkOffset > math.MaxInt16 || skbMarkOffset%4 != 0 || lanMark == 0 || outerMapFD < 0 || uint64(outerMapFD) > math.MaxInt32 {
		return nil, errExitLANBPFProgram
	}
	// Opcode and register encoding follow the Linux BPF ISA. Helper numbers
	// follow include/uapi/linux/bpf.h: map_lookup_elem=1, ktime_get_boot_ns=125.
	// r6 is callee-saved; r1..r5 are unavailable after every helper call.
	const accept, drop = 19, 21
	code := make([]byte, 0, 23*8)
	emit := func(opcode, dst, src byte, offset int16, immediate uint32) {
		var instruction [8]byte
		instruction[0] = opcode
		if order == binary.LittleEndian {
			instruction[1] = src<<4 | dst
		} else {
			instruction[1] = dst<<4 | src
		}
		order.PutUint16(instruction[2:], uint16(offset))
		order.PutUint32(instruction[4:], immediate)
		code = append(code, instruction[:]...)
	}
	emit(0x79, 6, 1, int16(ctxSKBOffset), 0)  // r6 = ctx->skb
	emit(0x61, 2, 6, int16(skbMarkOffset), 0) // r2 = skb->mark, zero-extended
	emit(0xb4, 3, 0, 0, lanMark)              // MOV32 avoids sign extension of marks >= 2^31.
	emit(0x5d, 2, 3, accept-3-1, 0)           // if mark != lanMark: accept
	emit(0x62, 10, 0, -4, 0)                  // *(u32 *)(fp-4) = key zero
	emit(0x18, 1, 1, 0, uint32(outerMapFD))   // LD_IMM64, BPF_PSEUDO_MAP_FD
	emit(0, 0, 0, 0, 0)                       // required wide-instruction continuation
	emit(0xbf, 2, 10, 0, 0)                   // r2 = fp
	emit(0x07, 2, 0, 0, 0xfffffffc)           // r2 -= 4
	emit(0x85, 0, 0, 0, 1)                    // lookup outer[0]
	emit(0x15, 0, 0, drop-10-1, 0)            // missing inner: drop
	emit(0xbf, 1, 0, 0, 0)                    // r1 = inner map
	emit(0xbf, 2, 10, 0, 0)
	emit(0x07, 2, 0, 0, 0xfffffffc)
	emit(0x85, 0, 0, 0, 1)         // lookup inner[0]
	emit(0x15, 0, 0, drop-15-1, 0) // missing value: drop
	emit(0x79, 6, 0, 0, 0)         // preserve full unsigned64 expiry across helper
	emit(0x85, 0, 0, 0, 125)       // CLOCK_BOOTTIME nanoseconds, includes suspend
	emit(0x3d, 0, 6, drop-18-1, 0) // unsigned now >= expiry: drop
	emit(0xb7, 0, 0, 0, 1)         // NF_ACCEPT
	emit(0x95, 0, 0, 0, 0)
	emit(0xb7, 0, 0, 0, 0) // NF_DROP
	emit(0x95, 0, 0, 0, 0)
	return code, nil
}
