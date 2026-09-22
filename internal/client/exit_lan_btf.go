package client

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var errExitLANBTF = errors.New("LAN kernel BTF layout is unavailable")

const (
	exitLANBTFMaxBytes = 64 << 20
	exitLANBTFMaxTypes = 1 << 20
	exitLANBTFMaxWork  = 8 << 20
)

type exitLANBTFType struct {
	name              string
	kind, size, count uint32
	flag              bool
	data              []byte
}

type exitLANBTF struct {
	order        binary.ByteOrder
	strings      []byte
	types        []exitLANBTFType
	work         int
	pointerBytes uint32
}

// exitLANBTFMarkLayout resolves metadata only; it neither loads BPF nor establishes
// permission to access a live kernel object. The caller must independently know
// the target kernel's pointer width: BTF does not encode it. Only the original
// 24-byte v1 header and known kinds 1..19 are supported; extensions fail closed.
// Wire definitions: include/uapi/linux/btf.h and docs.kernel.org/bpf/btf.html.
func exitLANBTFMarkLayout(raw []byte, pointerBytes uint32) (uint32, uint32, error) {
	if pointerBytes != 4 && pointerBytes != 8 {
		return 0, 0, errExitLANBTF
	}
	b, err := parseExitLANBTF(raw)
	if err != nil {
		return 0, 0, err
	}
	b.pointerBytes = pointerBytes
	var ctxID, skbID uint32
	for id, typ := range b.types {
		if typ.kind != 4 {
			continue
		}
		switch typ.name {
		case "bpf_nf_ctx":
			if ctxID != 0 {
				return 0, 0, errExitLANBTF
			}
			ctxID = uint32(id)
		case "sk_buff":
			if skbID != 0 {
				return 0, 0, errExitLANBTF
			}
			skbID = uint32(id)
		}
	}
	if ctxID == 0 || skbID == 0 {
		return 0, 0, errExitLANBTF
	}
	ctxMember, ctxOffset, err := b.member(ctxID, "skb", make(map[uint32]bool), 0)
	if err != nil {
		return 0, 0, err
	}
	ptr, err := b.resolve(ctxMember)
	if err != nil || b.types[ptr].kind != 2 {
		return 0, 0, errExitLANBTF
	}
	target, err := b.resolve(b.types[ptr].size)
	if err != nil || target != skbID || ctxOffset%pointerBytes != 0 || uint64(ctxOffset)+uint64(pointerBytes) > uint64(b.types[ctxID].size) || b.types[ctxID].size%pointerBytes != 0 {
		return 0, 0, errExitLANBTF
	}
	mark, markOffset, err := b.member(skbID, "mark", make(map[uint32]bool), 0)
	if err != nil {
		return 0, 0, err
	}
	mark, err = b.resolve(mark)
	if err != nil {
		return 0, 0, err
	}
	integer := b.types[mark]
	if integer.kind != 1 || integer.size != 4 || b.order.Uint32(integer.data) != 32 || markOffset%4 != 0 || uint64(markOffset)+4 > uint64(b.types[skbID].size) {
		return 0, 0, errExitLANBTF
	}
	return ctxOffset, markOffset, nil
}

func parseExitLANBTF(raw []byte) (*exitLANBTF, error) {
	if len(raw) < 24 || len(raw) > exitLANBTFMaxBytes {
		return nil, errExitLANBTF
	}
	var order binary.ByteOrder
	switch {
	case raw[0] == 0x9f && raw[1] == 0xeb:
		order = binary.LittleEndian
	case raw[0] == 0xeb && raw[1] == 0x9f:
		order = binary.BigEndian
	default:
		return nil, errExitLANBTF
	}
	if raw[2] != 1 || raw[3] != 0 || order.Uint32(raw[4:]) != 24 {
		return nil, errExitLANBTF
	}
	ts, tl := uint64(order.Uint32(raw[8:])), uint64(order.Uint32(raw[12:]))
	ss, sl := uint64(order.Uint32(raw[16:])), uint64(order.Uint32(raw[20:]))
	if ts%4 != 0 || tl%4 != 0 || tl == 0 || sl == 0 || ts+tl > uint64(len(raw)-24) || ss+sl > uint64(len(raw)-24) || ts < ss+sl && ss < ts+tl {
		return nil, errExitLANBTF
	}
	b := &exitLANBTF{order: order, strings: raw[24+ss : 24+ss+sl], types: []exitLANBTFType{{}}, work: exitLANBTFMaxWork}
	if b.strings[0] != 0 || b.strings[len(b.strings)-1] != 0 {
		return nil, errExitLANBTF
	}
	data := raw[24+ts : 24+ts+tl]
	for len(data) != 0 {
		if len(data) < 12 || len(b.types) >= exitLANBTFMaxTypes {
			return nil, errExitLANBTF
		}
		name, err := b.name(order.Uint32(data))
		if err != nil {
			return nil, err
		}
		info := order.Uint32(data[4:])
		// This bounded subset also rejects formerly reserved bits rather than
		// interpreting a future record using an older kind's payload layout.
		if info&0x60ff0000 != 0 {
			return nil, errExitLANBTF
		}
		t := exitLANBTFType{name: name, kind: info >> 24 & 31, count: info & 65535, flag: info>>31 != 0, size: order.Uint32(data[8:])}
		var extra uint64
		switch t.kind {
		case 1, 14, 17:
			extra = 4
		case 3:
			extra = 12
		case 4, 5, 15, 19:
			extra = uint64(t.count) * 12
		case 6, 13:
			extra = uint64(t.count) * 8
		case 2, 7, 8, 9, 10, 11, 12, 16, 18:
		default:
			return nil, errExitLANBTF
		}
		if extra > uint64(len(data)-12) {
			return nil, errExitLANBTF
		}
		t.data = data[12 : 12+extra]
		b.types = append(b.types, t)
		data = data[12+extra:]
	}
	for id := 1; id < len(b.types); id++ {
		if err := b.validate(b.types[id]); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (b *exitLANBTF) name(offset uint32) (string, error) {
	if uint64(offset) >= uint64(len(b.strings)) {
		return "", errExitLANBTF
	}
	part := b.strings[offset:]
	if len(part) > 4096 {
		part = part[:4096]
	}
	end := bytes.IndexByte(part, 0)
	if end < 0 || b.work < end+1 {
		return "", errExitLANBTF
	}
	b.work -= end + 1
	return string(part[:end]), nil
}

func (b *exitLANBTF) ref(id uint32) bool { return uint64(id) < uint64(len(b.types)) }

func (b *exitLANBTF) validate(t exitLANBTFType) error {
	if t.flag && t.kind != 4 && t.kind != 5 && t.kind != 6 && t.kind != 7 && t.kind != 19 {
		return errExitLANBTF
	}
	switch t.kind {
	case 4, 5, 6, 13, 15, 19:
	case 12:
		if t.count > 2 {
			return errExitLANBTF
		}
	default:
		if t.count != 0 {
			return errExitLANBTF
		}
	}
	switch t.kind {
	case 1:
		x := b.order.Uint32(t.data)
		if t.size == 0 || t.size > 16 || x&0xf800ff00 != 0 || x&255 == 0 || uint64(x&255)+uint64(x>>16&255) > uint64(t.size)*8 {
			return errExitLANBTF
		}
	case 2, 8, 9, 10, 11, 12, 13, 14, 17, 18:
		if !b.ref(t.size) {
			return errExitLANBTF
		}
	case 3:
		if t.size != 0 || !b.ref(b.order.Uint32(t.data)) || !b.ref(b.order.Uint32(t.data[4:])) {
			return errExitLANBTF
		}
	case 7:
		if t.size != 0 {
			return errExitLANBTF
		}
	}
	if (t.kind == 2 || t.kind == 3 || t.kind == 9 || t.kind == 10 || t.kind == 11 || t.kind == 13) && t.name != "" {
		return errExitLANBTF
	}
	if t.kind == 14 && b.order.Uint32(t.data) > 2 {
		return errExitLANBTF
	}
	stride := 0
	switch t.kind {
	case 4, 5, 15, 19:
		stride = 12
	case 6, 13:
		stride = 8
	}
	for pos := 0; stride != 0 && pos < len(t.data); pos += stride {
		if b.work <= 0 {
			return errExitLANBTF
		}
		b.work--
		row := t.data[pos:]
		if t.kind == 15 {
			if !b.ref(b.order.Uint32(row)) || uint64(b.order.Uint32(row[4:]))+uint64(b.order.Uint32(row[8:])) > uint64(t.size) {
				return errExitLANBTF
			}
			continue
		}
		if _, err := b.name(b.order.Uint32(row)); err != nil {
			return err
		}
		if (t.kind == 4 || t.kind == 5 || t.kind == 13) && !b.ref(b.order.Uint32(row[4:])) {
			return errExitLANBTF
		}
		if t.kind == 4 || t.kind == 5 {
			offset := b.order.Uint32(row[8:])
			width := uint32(0)
			if t.flag {
				width, offset = offset>>24, offset&0xffffff
			}
			if uint64(offset)+uint64(width) > uint64(t.size)*8 || t.kind == 5 && offset != 0 {
				return errExitLANBTF
			}
		}
	}
	return nil
}

func (b *exitLANBTF) resolve(id uint32) (uint32, error) {
	for depth := 0; depth < 64; depth++ {
		if id == 0 || !b.ref(id) || b.work <= 0 {
			return 0, errExitLANBTF
		}
		b.work--
		t := b.types[id]
		switch t.kind {
		case 8, 9, 10, 11, 18:
			id = t.size
		default:
			return id, nil
		}
	}
	return 0, errExitLANBTF
}

// member promotes only anonymous aggregates, as C does. Named nested fields do
// not become top-level members. Union alternatives may overlap, but two visible
// occurrences of the requested name are always ambiguous, even at equal offsets.
func (b *exitLANBTF) member(id uint32, name string, visiting map[uint32]bool, depth int) (uint32, uint32, error) {
	if depth >= 64 || visiting[id] || !b.ref(id) {
		return 0, 0, errExitLANBTF
	}
	t := b.types[id]
	if t.kind != 4 && t.kind != 5 {
		return 0, 0, errExitLANBTF
	}
	visiting[id] = true
	defer delete(visiting, id)
	var found, offset uint32
	selectedPos := -1
	for pos := 0; pos < len(t.data); pos += 12 {
		if b.work <= 0 {
			return 0, 0, errExitLANBTF
		}
		b.work--
		row := t.data[pos:]
		field, err := b.name(b.order.Uint32(row))
		if err != nil {
			return 0, 0, err
		}
		if field != name && field != "" {
			continue
		}
		memberID := b.order.Uint32(row[4:])
		resolved, err := b.resolve(memberID)
		if err != nil {
			return 0, 0, err
		}
		// Unnamed scalar padding is not a promoted C member. Its bit range
		// remains checked by validate and by the sibling overlap check below.
		if field == "" && b.types[resolved].kind != 4 && b.types[resolved].kind != 5 {
			continue
		}
		bitOffset := b.order.Uint32(row[8:])
		if t.flag {
			if bitOffset>>24 != 0 {
				return 0, 0, errExitLANBTF
			}
			bitOffset &= 0xffffff
		}
		if bitOffset%8 != 0 {
			return 0, 0, errExitLANBTF
		}
		at := bitOffset / 8
		if field == "" {
			inner := b.types[resolved]
			if uint64(at)+uint64(inner.size) > uint64(t.size) {
				return 0, 0, errExitLANBTF
			}
			child, childOffset, childErr := b.member(resolved, name, visiting, depth+1)
			if childErr != nil {
				return 0, 0, childErr
			}
			if child == 0 {
				continue
			}
			memberID, at = child, at+childOffset
		} else {
			width := b.types[resolved].size
			if b.types[resolved].kind == 2 {
				width = b.pointerBytes
			}
			kind := b.types[resolved].kind
			if kind != 1 && kind != 2 && kind != 4 && kind != 5 || width == 0 || (kind == 1 || kind == 2) && at%width != 0 || uint64(at)+uint64(width) > uint64(t.size) {
				return 0, 0, errExitLANBTF
			}
		}
		if found != 0 {
			return 0, 0, errExitLANBTF
		}
		found, offset = memberID, at
		selectedPos = pos
	}
	if found != 0 && t.kind == 4 {
		width, err := b.sizeOf(found, 0)
		if err != nil {
			return 0, 0, err
		}
		for pos := 0; pos < len(t.data); pos += 12 {
			if pos == selectedPos {
				continue
			}
			row := t.data[pos:]
			start := uint64(b.order.Uint32(row[8:]))
			var bits uint64
			if t.flag {
				bits, start = start>>24, start&0xffffff
			}
			if bits == 0 {
				size, sizeErr := b.sizeOf(b.order.Uint32(row[4:]), 0)
				if sizeErr != nil {
					return 0, 0, sizeErr
				}
				bits = size * 8
			}
			if start+bits > uint64(t.size)*8 || bits != 0 && start < (uint64(offset)+width)*8 && uint64(offset)*8 < start+bits {
				return 0, 0, errExitLANBTF
			}
		}
	}
	return found, offset, nil
}

func (b *exitLANBTF) sizeOf(id uint32, depth int) (uint64, error) {
	if depth >= 64 {
		return 0, errExitLANBTF
	}
	id, err := b.resolve(id)
	if err != nil {
		return 0, err
	}
	t := b.types[id]
	switch t.kind {
	case 1, 4, 5, 6, 16, 19:
		return uint64(t.size), nil
	case 2:
		return uint64(b.pointerBytes), nil
	case 3:
		size, sizeErr := b.sizeOf(b.order.Uint32(t.data), depth+1)
		if sizeErr != nil {
			return 0, sizeErr
		}
		size *= uint64(b.order.Uint32(t.data[8:]))
		if size > 0xffffffff {
			return 0, errExitLANBTF
		}
		return size, nil
	default:
		return 0, errExitLANBTF
	}
}
