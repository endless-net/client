package client

import (
	"encoding/binary"
	"math"
)

const exitLANHookMessage = 12 << 8

type exitLANHookDump struct {
	order                  binary.ByteOrder
	sequence, port         uint32
	expected               exitLANBPFLinkIdentity
	bytes, messages, found int
	done, failed           bool
}

func newExitLANHookDump(order binary.ByteOrder, sequence, port uint32, expected exitLANBPFLinkIdentity) (*exitLANHookDump, error) {
	if (order != binary.LittleEndian && order != binary.BigEndian) || sequence == 0 || port == 0 || expected.id == 0 || expected.programID == 0 || (expected.family != 2 && expected.family != 10) || expected.hook >= 5 || expected.priority == math.MinInt32 || expected.priority == math.MaxInt32 {
		return nil, errExitLANBPF
	}
	return &exitLANHookDump{order: order, sequence: sequence, port: port, expected: expected}, nil
}

func (d *exitLANHookDump) request() []byte {
	raw := make([]byte, 28)
	d.order.PutUint32(raw, 28)
	d.order.PutUint16(raw[4:], exitLANHookMessage)
	d.order.PutUint16(raw[6:], 0x301) // REQUEST | ROOT | MATCH (DUMP), no ACK.
	d.order.PutUint32(raw[8:], d.sequence)
	d.order.PutUint32(raw[12:], d.port)
	raw[16] = byte(d.expected.family)
	d.order.PutUint16(raw[20:], 8)
	d.order.PutUint16(raw[22:], 1)
	binary.BigEndian.PutUint32(raw[24:], d.expected.hook)
	return raw
}

// consume proves only a completed, bounded dump at observation time. Kernel
// sender identity is supplied by recvmsg, not inferred from nlmsg_pid. Hook
// records use PID zero; DONE carries the requester's port ID. A noninterrupted
// dump is not an immutable generation or an atomic snapshot across families.
func (d *exitLANHookDump) consume(raw []byte, kernelSender, truncated bool) (result error) {
	defer func() {
		if result != nil {
			d.failed = true
		}
	}()
	if d.failed || d.done || !kernelSender || truncated || len(raw) == 0 || len(raw) > 64<<10 || d.bytes+len(raw) > 1<<20 {
		return errExitLANBPF
	}
	d.bytes += len(raw)
	for len(raw) > 0 {
		if d.done || len(raw) < 16 || d.messages >= 8192 {
			return errExitLANBPF
		}
		d.messages++
		size := int(d.order.Uint32(raw))
		kind := d.order.Uint16(raw[4:])
		flags := d.order.Uint16(raw[6:])
		if size < 16 || size > len(raw) || d.order.Uint32(raw[8:]) != d.sequence || flags != 2 {
			return errExitLANBPF
		}
		step := (size + 3) &^ 3
		if step > len(raw) {
			return errExitLANBPF
		}
		for _, pad := range raw[size:step] {
			if pad != 0 {
				return errExitLANBPF
			}
		}
		switch kind {
		case 3: // NLMSG_DONE; zero ACK is deliberately not completion.
			if size != 20 || d.order.Uint32(raw[12:]) != d.port || d.order.Uint32(raw[16:]) != 0 {
				return errExitLANBPF
			}
			d.done = true
		case exitLANHookMessage:
			if size < 20 || d.order.Uint32(raw[12:]) != 0 || raw[16] != byte(d.expected.family) || raw[17] != 0 || raw[18] != 0 || raw[19] != 0 {
				return errExitLANBPF
			}
			program, priority, err := d.hookRecord(raw[20:size])
			if err != nil {
				return err
			}
			if program == d.expected.programID {
				if priority != d.expected.priority {
					return errExitLANBPF
				}
				d.found++
				if d.found > 1 {
					return errExitLANBPF
				}
			}
		default:
			return errExitLANBPF
		}
		raw = raw[step:]
	}
	return nil
}

func (d *exitLANHookDump) confirmed() bool { return !d.failed && d.done && d.found == 1 }

type exitLANHookAttribute struct {
	flags uint16
	value []byte
}

func exitLANHookAttributes(raw []byte, order binary.ByteOrder) (map[uint16]exitLANHookAttribute, error) {
	attrs := make(map[uint16]exitLANHookAttribute)
	for len(raw) > 0 {
		if len(raw) < 4 || len(attrs) >= 32 {
			return nil, errExitLANBPF
		}
		size := int(order.Uint16(raw))
		kind := order.Uint16(raw[2:])
		id := kind & 0x3fff
		if size < 4 || size > len(raw) || id == 0 {
			return nil, errExitLANBPF
		}
		if _, exists := attrs[id]; exists {
			return nil, errExitLANBPF
		}
		step := (size + 3) &^ 3
		if step > len(raw) {
			return nil, errExitLANBPF
		}
		for _, pad := range raw[size:step] {
			if pad != 0 {
				return nil, errExitLANBPF
			}
		}
		attrs[id] = exitLANHookAttribute{kind & 0xc000, raw[4:size]}
		raw = raw[step:]
	}
	return attrs, nil
}

func exitLANHookNumber(a exitLANHookAttribute) (uint32, bool) {
	// nla_put_be32 emits a BE payload without NLA_F_NET_BYTEORDER.
	if a.flags != 0 || len(a.value) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(a.value), true
}

func (d *exitLANHookDump) hookRecord(raw []byte) (uint32, int32, error) {
	attrs, err := exitLANHookAttributes(raw, d.order)
	if err != nil {
		return 0, 0, err
	}
	hook, ok := exitLANHookNumber(attrs[1])
	if !ok || hook != d.expected.hook {
		return 0, 0, errExitLANBPF
	}
	priority, ok := exitLANHookNumber(attrs[2])
	if !ok {
		return 0, 0, errExitLANBPF
	}
	info, exists := attrs[6]
	if !exists {
		return 0, int32(priority), nil
	} // Ordinary non-BPF hook.
	if info.flags != 0x8000 {
		return 0, 0, errExitLANBPF
	}
	nested, err := exitLANHookAttributes(info.value, d.order)
	if err != nil {
		return 0, 0, err
	}
	typeID, ok := exitLANHookNumber(nested[2])
	if !ok {
		return 0, 0, errExitLANBPF
	}
	desc, ok := nested[1]
	if !ok || desc.flags != 0x8000 {
		return 0, 0, errExitLANBPF
	}
	if typeID != 2 {
		return 0, int32(priority), nil
	} // nft/NAT metadata is not BPF authority.
	programAttrs, err := exitLANHookAttributes(desc.value, d.order)
	if err != nil {
		return 0, 0, err
	}
	program, ok := exitLANHookNumber(programAttrs[1])
	if !ok || program == 0 || len(programAttrs) != 1 || len(nested) != 2 {
		return 0, 0, errExitLANBPF
	}
	return program, int32(priority), nil
}
