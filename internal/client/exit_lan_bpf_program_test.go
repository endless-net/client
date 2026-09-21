package client

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

type exitLANBPFTestInstruction struct {
	op, dst, src byte
	offset       int16
	imm          uint32
}

// This deliberately small interpreter is test-only, bounded, and rejects every
// instruction outside the emitted subset. It is not a replacement for the
// kernel verifier or native netfilter attachment qualification.
func decodeExitLANBPFTest(code []byte, order binary.ByteOrder) ([]exitLANBPFTestInstruction, error) {
	if len(code) == 0 || len(code)%8 != 0 || len(code) > 64*8 {
		return nil, fmt.Errorf("invalid program size")
	}
	decoded := make([]exitLANBPFTestInstruction, len(code)/8)
	continuation := make(map[int]bool)
	for pc := range decoded {
		row := code[pc*8:]
		dst, src := row[1]&15, row[1]>>4
		if order == binary.BigEndian {
			dst, src = src, dst
		}
		i := exitLANBPFTestInstruction{row[0], dst, src, int16(order.Uint16(row[2:])), order.Uint32(row[4:])}
		decoded[pc] = i
		if continuation[pc] {
			if i != (exitLANBPFTestInstruction{}) {
				return nil, fmt.Errorf("invalid wide continuation")
			}
			continue
		}
		if dst > 10 || src > 10 {
			return nil, fmt.Errorf("invalid register")
		}
		switch i.op {
		case 0x79, 0x61:
			if i.imm != 0 || dst == 10 {
				return nil, fmt.Errorf("invalid load")
			}
		case 0x62:
			if src != 0 {
				return nil, fmt.Errorf("invalid store")
			}
		case 0xb4, 0xb7, 0x07:
			if src != 0 || i.offset != 0 || dst == 10 {
				return nil, fmt.Errorf("invalid immediate arithmetic")
			}
		case 0xbf:
			if i.imm != 0 || i.offset != 0 || dst == 10 {
				return nil, fmt.Errorf("invalid register move")
			}
		case 0x18:
			if src != 1 || dst == 10 || i.offset != 0 || pc+1 >= len(decoded) {
				return nil, fmt.Errorf("invalid map reference")
			}
			continuation[pc+1] = true
		case 0x85:
			if dst != 0 || src != 0 || i.offset != 0 || i.imm != 1 && i.imm != 125 {
				return nil, fmt.Errorf("invalid helper")
			}
		case 0x95:
			if dst != 0 || src != 0 || i.offset != 0 || i.imm != 0 {
				return nil, fmt.Errorf("invalid exit")
			}
		case 0x5d, 0x3d:
			if i.imm != 0 {
				return nil, fmt.Errorf("invalid register branch")
			}
		case 0x15:
			if src != 0 {
				return nil, fmt.Errorf("invalid immediate branch")
			}
		default:
			return nil, fmt.Errorf("unsupported opcode %x", i.op)
		}
	}
	for pc, i := range decoded {
		if i.op != 0x5d && i.op != 0x3d && i.op != 0x15 {
			continue
		}
		target := pc + 1 + int(i.offset)
		if target <= pc || target >= len(decoded) || continuation[target] {
			return nil, fmt.Errorf("invalid branch target")
		}
	}
	return decoded, nil
}

type exitLANBPFTestRuntime struct {
	mark            uint32
	inner, value    bool
	expiry, now     uint64
	lookups, clocks int
}

func runExitLANBPFTest(code []byte, order binary.ByteOrder, ctxOffset, markOffset uint32, fd int, env *exitLANBPFTestRuntime) (uint64, error) {
	program, err := decodeExitLANBPFTest(code, order)
	if err != nil {
		return 0, err
	}
	const ctx, skb, frame, outer, inner, value = uint64(0x10000), uint64(0x20000), uint64(0x30000), uint64(0x40000), uint64(0x50000), uint64(0x60000)
	var r [11]uint64
	r[1], r[10] = ctx, frame
	keyReady := false
	for pc, steps := 0, 0; steps < 64; steps++ {
		if pc < 0 || pc >= len(program) {
			return 0, fmt.Errorf("execution outside program")
		}
		i := program[pc]
		next := pc + 1
		switch i.op {
		case 0x79, 0x61:
			address := r[i.src] + uint64(int64(i.offset))
			switch {
			case i.op == 0x79 && address == ctx+uint64(ctxOffset):
				r[i.dst] = skb
			case i.op == 0x61 && address == skb+uint64(markOffset):
				r[i.dst] = uint64(env.mark)
			case i.op == 0x79 && address == value && env.value:
				r[i.dst] = env.expiry
			default:
				return 0, fmt.Errorf("invalid memory read at %x", address)
			}
		case 0x62:
			if r[i.dst]+uint64(int64(i.offset)) != frame-4 || i.imm != 0 {
				return 0, fmt.Errorf("invalid key write")
			}
			keyReady = true
		case 0xb4:
			r[i.dst] = uint64(i.imm)
		case 0xb7:
			r[i.dst] = uint64(int64(int32(i.imm)))
		case 0xbf:
			r[i.dst] = r[i.src]
		case 0x07:
			r[i.dst] += uint64(int64(int32(i.imm)))
		case 0x18:
			if i.imm != uint32(fd) {
				return 0, fmt.Errorf("foreign map fd")
			}
			r[i.dst] = outer
			next++
		case 0x85:
			var result uint64
			if i.imm == 1 {
				env.lookups++
				if !keyReady || r[2] != frame-4 {
					return 0, fmt.Errorf("lookup without initialized key zero")
				}
				switch r[1] {
				case outer:
					if env.inner {
						result = inner
					}
				case inner:
					if env.inner && env.value {
						result = value
					}
				default:
					return 0, fmt.Errorf("lookup using foreign map")
				}
			} else {
				env.clocks++
				result = env.now
			}
			// Helpers may destroy every caller-saved register. No fake helper
			// is allowed to accidentally preserve a key pointer or an expiry.
			for j := 1; j <= 5; j++ {
				r[j] = 0xdead0000 + uint64(j)
			}
			r[0] = result
		case 0x5d:
			if r[i.dst] != r[i.src] {
				next += int(i.offset)
			}
		case 0x15:
			if r[i.dst] == uint64(int64(int32(i.imm))) {
				next += int(i.offset)
			}
		case 0x3d:
			if r[i.dst] >= r[i.src] {
				next += int(i.offset)
			}
		case 0x95:
			return r[0], nil
		default:
			return 0, fmt.Errorf("execution entered continuation")
		}
		pc = next
	}
	return 0, fmt.Errorf("execution budget exceeded")
}

func TestExitLANBPFProgramDeadline(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, mark := range []uint32{51821, 0x80000000, math.MaxUint32} {
			code, err := buildExitLANBPFProgram(8, 192, mark, 17, order)
			if err != nil {
				t.Fatal(err)
			}
			for name, test := range map[string]struct {
				env             exitLANBPFTestRuntime
				verdict         uint64
				lookups, clocks int
			}{
				"other mark":       {exitLANBPFTestRuntime{mark: mark - 1}, 1, 0, 0},
				"no inner":         {exitLANBPFTestRuntime{mark: mark}, 0, 1, 0},
				"no lease":         {exitLANBPFTestRuntime{mark: mark, inner: true}, 0, 2, 0},
				"zero":             {exitLANBPFTestRuntime{mark: mark, inner: true, value: true}, 0, 2, 1},
				"expired":          {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: 4, now: 5}, 0, 2, 1},
				"equal":            {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: 5, now: 5}, 0, 2, 1},
				"future":           {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: 6, now: 5}, 1, 2, 1},
				"full64":           {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: 1<<63 + 1, now: 1 << 63}, 1, 2, 1},
				"unsigned expired": {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: 1, now: 1 << 63}, 0, 2, 1},
				"maximum":          {exitLANBPFTestRuntime{mark: mark, inner: true, value: true, expiry: math.MaxUint64, now: math.MaxUint64 - 1}, 1, 2, 1},
			} {
				t.Run(fmt.Sprintf("%s/%08x/%s", order, mark, name), func(t *testing.T) {
					got, err := runExitLANBPFTest(code, order, 8, 192, 17, &test.env)
					if err != nil || got != test.verdict || test.env.lookups != test.lookups || test.env.clocks != test.clocks {
						t.Fatalf("verdict=%d lookups=%d clocks=%d error=%v", got, test.env.lookups, test.env.clocks, err)
					}
				})
			}
		}
	}
}

func TestExitLANBPFProgramDoesNotRenewDeadline(t *testing.T) {
	code, err := buildExitLANBPFProgram(0, 0, 7, 0, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	for _, now := range []uint64{9, 10, 11, 1000} {
		env := exitLANBPFTestRuntime{mark: 7, inner: true, value: true, expiry: 10, now: now}
		got, err := runExitLANBPFTest(code, binary.LittleEndian, 0, 0, 0, &env)
		want := uint64(0)
		if now < 10 {
			want = 1
		}
		if err != nil || got != want || env.expiry != 10 {
			t.Fatalf("now=%d result=%d expiry=%d error=%v", now, got, env.expiry, err)
		}
	}
}

func TestExitLANBPFProgramBoundsAndEncoding(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		code, err := buildExitLANBPFProgram(32760, 32764, 0xfedcba98, math.MaxInt32, order)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 23*8 {
			t.Fatalf("unexpected instruction count %d", len(code)/8)
		}
		regs := byte(0x16)
		if order == binary.BigEndian {
			regs = 0x61
		}
		if code[0] != 0x79 || code[1] != regs || order.Uint16(code[2:]) != 32760 || order.Uint32(code[2*8+4:]) != 0xfedcba98 {
			t.Fatal("instruction ABI mismatch")
		}
		if _, err := decodeExitLANBPFTest(code, order); err != nil {
			t.Fatal(err)
		}
		for name, mutate := range map[string]func([]byte){
			"bad register":                func(raw []byte) { raw[1] = 0xff },
			"jump into wide continuation": func(raw []byte) { order.PutUint16(raw[3*8+2:], 2) },
			"backwards jump":              func(raw []byte) { order.PutUint16(raw[3*8+2:], 0xffff) },
			"jump beyond program":         func(raw []byte) { order.PutUint16(raw[3*8+2:], 32767) },
			"dirty continuation":          func(raw []byte) { raw[6*8+4] = 1 },
		} {
			t.Run(fmt.Sprintf("%s/%s", order, name), func(t *testing.T) {
				raw := append([]byte(nil), code...)
				mutate(raw)
				if _, err := decodeExitLANBPFTest(raw, order); err == nil {
					t.Fatal("malformed instruction accepted")
				}
			})
		}
	}
	for _, args := range [][4]int64{{1, 4, 1, 1}, {8, 1, 1, 1}, {32768, 4, 1, 1}, {8, 32768, 1, 1}, {8, 4, 0, 1}, {8, 4, 1, -1}} {
		if _, err := buildExitLANBPFProgram(uint32(args[0]), uint32(args[1]), uint32(args[2]), int(args[3]), binary.LittleEndian); err == nil {
			t.Fatalf("invalid input accepted: %v", args)
		}
	}
	if _, err := buildExitLANBPFProgram(8, 4, 1, 1, nil); err == nil {
		t.Fatal("unspecified byte order accepted")
	}
	if uint64(^uint(0)>>1) > math.MaxInt32 {
		tooLarge := int64(math.MaxInt32) + 1
		if _, err := buildExitLANBPFProgram(8, 4, 1, int(tooLarge), binary.LittleEndian); err == nil {
			t.Fatal("oversized map fd accepted")
		}
	}
}
