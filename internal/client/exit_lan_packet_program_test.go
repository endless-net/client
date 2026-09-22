package client

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// Executes the emitted bytes, including helper clobbers and memory faults.
// This is bounded instruction evidence; kernel verifier acceptance is separate.
func runExitLANPacketTest(code []byte, order binary.ByteOrder, memory map[uint64]byte, now uint64, missing bool) (uint64, error) {
	var r [11]uint64
	r[1], r[10] = 0x10000, 0x90000
	read := func(address uint64, size int) (uint64, bool) {
		var value [8]byte
		for i := 0; i < size; i++ {
			b, ok := memory[address+uint64(i)]
			if !ok {
				return 0, false
			}
			value[i] = b
		}
		switch size {
		case 1:
			return uint64(value[0]), true
		case 2:
			return uint64(order.Uint16(value[:])), true
		case 4:
			return uint64(order.Uint32(value[:])), true
		default:
			return order.Uint64(value[:]), true
		}
	}
	write := func(address uint64, size int, value uint64) {
		var b [8]byte
		switch size {
		case 4:
			order.PutUint32(b[:], uint32(value))
		default:
			order.PutUint64(b[:], value)
		}
		for i := 0; i < size; i++ {
			memory[address+uint64(i)] = b[i]
		}
	}
	for pc, steps := 0, 0; steps < 4096; steps++ {
		if pc < 0 || pc*8+8 > len(code) {
			return 0, fmt.Errorf("invalid program counter")
		}
		row := code[pc*8 : pc*8+8]
		op, dst, src := row[0], row[1]&15, row[1]>>4
		if order == binary.BigEndian {
			dst, src = src, dst
		}
		if dst > 10 || src > 10 {
			return 0, fmt.Errorf("invalid register")
		}
		offset, imm := int16(order.Uint16(row[2:])), order.Uint32(row[4:])
		next := pc + 1
		signed := uint64(int64(int32(imm)))
		switch op {
		case 0x79, 0x61, 0x71, 0x69:
			size := 8
			if op == 0x61 {
				size = 4
			}
			if op == 0x71 {
				size = 1
			}
			if op == 0x69 {
				size = 2
			}
			value, ok := read(r[src]+uint64(int64(offset)), size)
			if !ok {
				return 0, fmt.Errorf("invalid direct read")
			}
			r[dst] = value
		case 0x7b:
			write(r[dst]+uint64(int64(offset)), 8, r[src])
		case 0x7a:
			write(r[dst]+uint64(int64(offset)), 8, signed)
		case 0x62:
			write(r[dst]+uint64(int64(offset)), 4, uint64(imm))
		case 0xbf:
			r[dst] = r[src]
		case 0xb7:
			r[dst] = signed
		case 0xb4:
			r[dst] = uint64(imm)
		case 0x07:
			r[dst] += signed
		case 0x57:
			r[dst] &= signed
		case 0x18:
			if (pc+2)*8 > len(code) {
				return 0, fmt.Errorf("truncated wide load")
			}
			r[dst] = uint64(imm) | uint64(order.Uint32(code[(pc+1)*8+4:]))<<32
			if src == 1 {
				r[dst] = 0xa0000
			}
			next++
		case 0x05:
			next += int(offset)
		case 0x15:
			if r[dst] == signed {
				next += int(offset)
			}
		case 0x55:
			if r[dst] != signed {
				next += int(offset)
			}
		case 0x1d:
			if r[dst] == r[src] {
				next += int(offset)
			}
		case 0x5d:
			if r[dst] != r[src] {
				next += int(offset)
			}
		case 0x3d:
			if r[dst] >= r[src] {
				next += int(offset)
			}
		case 0x85:
			var result uint64
			switch imm {
			case 1:
				key, ok := read(r[2], 4)
				if !ok || key != 0 {
					return 0, fmt.Errorf("invalid map key")
				}
				if !missing {
					switch r[1] {
					case 0xa0000:
						result = 0xb0000
					case 0xb0000:
						result = 0xc0000
					default:
						return 0, fmt.Errorf("foreign map")
					}
				}
			case 125:
				result = now
			case 113:
				value, ok := read(r[3], int(r[2]))
				if !ok {
					result = ^uint64(0)
				} else {
					var b [8]byte
					switch r[2] {
					case 1:
						b[0] = byte(value)
					case 2:
						order.PutUint16(b[:], uint16(value))
					case 4:
						order.PutUint32(b[:], uint32(value))
					case 8:
						order.PutUint64(b[:], value)
					default:
						return 0, fmt.Errorf("probe size")
					}
					for i := uint64(0); i < r[2]; i++ {
						memory[r[1]+i] = b[i]
					}
				}
			default:
				return 0, fmt.Errorf("unexpected helper")
			}
			for i := 1; i <= 5; i++ {
				r[i] = 0xdead0000 + uint64(i)
			}
			r[0] = result
		case 0x95:
			return r[0], nil
		default:
			return 0, fmt.Errorf("unexpected instruction %x", op)
		}
		pc = next
	}
	return 0, fmt.Errorf("execution limit")
}

func TestExitLANPacketGateChecksFinalDeviceAndGateway(t *testing.T) {
	l := exitLANPacketLayout{ctxSKB: 8, ctxState: 0, skbMark: 32, skbDst: 40, stateOut: 8, stateFamily: 0, deviceIndex: 8, deviceSD: 16, kernfsID: 8, dstDevice: 0, ipv4Gateway: 16, ipv4Type: 18, ipv6Flags: 24}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		code, err := buildExitLANPacketProgram(l, []exitLANPacketDevice{{2, 0xabcdef0100000002}, {3, 0x1234567800000003}}, 0x80000001, 17, order)
		if err != nil {
			t.Fatal(err)
		}
		for _, scenario := range []string{"ipv4", "ipv6", "second_device", "other_mark", "expired", "missing", "gateway4", "gateway6", "non_unicast", "replacement", "wrong_index", "wrong_dst_device", "missing_sysfs", "read_fault", "unknown_family"} {
			t.Run(fmt.Sprintf("%s/%s", order, scenario), func(t *testing.T) {
				memory := map[uint64]byte{}
				put := func(at uint64, size int, v uint64) {
					var b [8]byte
					switch size {
					case 1:
						b[0] = byte(v)
					case 2:
						order.PutUint16(b[:], uint16(v))
					case 4:
						order.PutUint32(b[:], uint32(v))
					case 8:
						order.PutUint64(b[:], v)
					}
					for i := 0; i < size; i++ {
						memory[at+uint64(i)] = b[i]
					}
				}
				put(0x10000, 8, 0x30000)
				put(0x10008, 8, 0x20000)
				put(0x20020, 4, 0x80000001)
				put(0x20028, 8, 0x60001)
				put(0x30000, 1, 2)
				put(0x30008, 8, 0x40000)
				put(0x40008, 4, 2)
				put(0x40010, 8, 0x50000)
				put(0x50008, 8, 0xabcdef0100000002)
				put(0x60000, 8, 0x40000)
				put(0x60010, 1, 0)
				put(0x60012, 2, 1)
				put(0x60018, 4, 0)
				put(0xc0000, 8, 20)
				now := uint64(10)
				switch scenario {
				case "ipv6":
					put(0x30000, 1, 10)
				case "second_device":
					put(0x40008, 4, 3)
					put(0x50008, 8, 0x1234567800000003)
				case "other_mark":
					put(0x20020, 4, 7)
				case "expired":
					now = 20
				case "gateway4":
					put(0x60010, 1, 1)
				case "gateway6":
					put(0x30000, 1, 10)
					put(0x60018, 4, 2)
				case "non_unicast":
					put(0x60012, 2, 2)
				case "replacement":
					put(0x50008, 8, 0xabcdef0200000002)
				case "wrong_index":
					put(0x40008, 4, 3)
				case "wrong_dst_device":
					put(0x60000, 8, 0x40001)
				case "missing_sysfs":
					put(0x40010, 8, 0)
				case "read_fault":
					delete(memory, 0x50008)
				case "unknown_family":
					put(0x30000, 1, 7)
				}
				verdict, err := runExitLANPacketTest(code, order, memory, now, scenario == "missing")
				want := uint64(0)
				if scenario == "ipv4" || scenario == "ipv6" || scenario == "second_device" || scenario == "other_mark" {
					want = 1
				}
				if err != nil || verdict != want {
					t.Fatal("unexpected packet verdict", verdict, err)
				}
			})
		}
	}
}
