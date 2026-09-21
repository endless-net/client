package client

import (
	"encoding/binary"
	"testing"
)

type exitLANBTFTestBuilder struct {
	order   binary.ByteOrder
	strings []byte
	types   []uint32
	ids     uint32
}

func (b *exitLANBTFTestBuilder) name(name string) uint32 {
	if b.strings == nil {
		b.strings = []byte{0}
	}
	if name == "" {
		return 0
	}
	id := uint32(len(b.strings))
	b.strings = append(b.strings, []byte(name)...)
	b.strings = append(b.strings, 0)
	return id
}

func (b *exitLANBTFTestBuilder) add(name string, kind, count, size uint32, data ...uint32) uint32 {
	b.types = append(b.types, b.name(name), kind<<24|count, size)
	b.types = append(b.types, data...)
	b.ids++
	return b.ids
}

func (b *exitLANBTFTestBuilder) raw() []byte {
	raw := make([]byte, 24+len(b.types)*4+len(b.strings))
	b.order.PutUint16(raw, 0xeb9f)
	raw[2] = 1
	b.order.PutUint32(raw[4:], 24)
	b.order.PutUint32(raw[12:], uint32(len(b.types)*4))
	b.order.PutUint32(raw[16:], uint32(len(b.types)*4))
	b.order.PutUint32(raw[20:], uint32(len(b.strings)))
	for i, word := range b.types {
		b.order.PutUint32(raw[24+i*4:], word)
	}
	copy(raw[24+len(b.types)*4:], b.strings)
	return raw
}

func exitLANBTFTestFixture(order binary.ByteOrder) *exitLANBTFTestBuilder {
	b := &exitLANBTFTestBuilder{order: order}
	b.add("unsigned int", 1, 0, 4, 32)                                          // 1
	b.add("__u32", 8, 0, 1)                                                     // 2
	b.add("", 5, 2, 4, b.name("mark"), 2, 0, b.name("reserved_tailroom"), 2, 0) // 3
	b.add("sk_buff", 4, 1, 128, 0, 3, 64*8)                                     // 4
	b.add("", 10, 0, 4)                                                         // 5
	b.add("", 2, 0, 5)                                                          // 6
	b.add("bpf_nf_ctx", 4, 1, 16, b.name("skb"), 6, 8*8)                        // 7
	return b
}

func TestExitLANBTFMarkLayout(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, pointer := range []uint32{4, 8} {
			ctx, mark, err := exitLANBTFMarkLayout(exitLANBTFTestFixture(order).raw(), pointer)
			if err != nil || ctx != 8 || mark != 64 {
				t.Fatalf("order=%v pointer=%d: %d %d %v", order, pointer, ctx, mark, err)
			}
		}
	}
	b := &exitLANBTFTestBuilder{order: binary.LittleEndian}
	b.add("u32", 1, 0, 4, 32)
	b.add("sk_buff", 4, 1, 20, b.name("mark"), 1, 16*8)
	b.add("", 2, 0, 2)
	b.add("bpf_nf_ctx", 4, 1, 8, b.name("skb"), 3, 4*8)
	ctx, mark, err := exitLANBTFMarkLayout(b.raw(), 4)
	if err != nil || ctx != 4 || mark != 16 {
		t.Fatalf("direct 32-bit layout: %d %d %v", ctx, mark, err)
	}
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err == nil {
		t.Fatal("32-bit layout accepted as 64-bit")
	}
}

func TestExitLANBTFRejectsUnsafeLayouts(t *testing.T) {
	tests := map[string]func(*exitLANBTFTestBuilder){
		"duplicate context":        func(b *exitLANBTFTestBuilder) { b.add("bpf_nf_ctx", 4, 0, 16) },
		"duplicate skb":            func(b *exitLANBTFTestBuilder) { b.add("sk_buff", 4, 0, 128) },
		"signed mark":              func(b *exitLANBTFTestBuilder) { b.types[3] |= 1 << 24 },
		"bool mark":                func(b *exitLANBTFTestBuilder) { b.types[3] |= 4 << 24 },
		"narrow mark":              func(b *exitLANBTFTestBuilder) { b.types[3] = 16 },
		"integer bit offset":       func(b *exitLANBTFTestBuilder) { b.types[3] |= 1 << 16 },
		"qualifier cycle":          func(b *exitLANBTFTestBuilder) { b.types[6] = 2 },
		"wrong pointer target":     func(b *exitLANBTFTestBuilder) { b.types[27] = 1 },
		"missing mark":             func(b *exitLANBTFTestBuilder) { b.types[10] = b.name("other") },
		"ambiguous union":          func(b *exitLANBTFTestBuilder) { b.types[13] = b.name("mark") },
		"anonymous cycle":          func(b *exitLANBTFTestBuilder) { b.types[20] = 4 },
		"anonymous outside parent": func(b *exitLANBTFTestBuilder) { b.types[21] = 128 * 8 },
		"small anonymous union":    func(b *exitLANBTFTestBuilder) { b.types[9] = 1 },
		"misaligned mark":          func(b *exitLANBTFTestBuilder) { b.types[21] = 63 * 8 },
		"bitfield mark":            func(b *exitLANBTFTestBuilder) { b.types[8] |= 1 << 31; b.types[12] = 32 << 24 },
		"bit offset skb":           func(b *exitLANBTFTestBuilder) { b.types[33] = 65 },
		"pointer outside ctx":      func(b *exitLANBTFTestBuilder) { b.types[30] = 8 },
		"unresolved type":          func(b *exitLANBTFTestBuilder) { b.types[32] = 999999 },
		"bad name":                 func(b *exitLANBTFTestBuilder) { b.types[28] = 0xffffffff },
		"unknown kind":             func(b *exitLANBTFTestBuilder) { b.types[1] = 31 << 24 },
		"reserved info bits":       func(b *exitLANBTFTestBuilder) { b.types[1] |= 1 << 16 },
		"nonzero pointer vlen":     func(b *exitLANBTFTestBuilder) { b.types[26] |= 1 },
		"overlapping context sibling": func(b *exitLANBTFTestBuilder) {
			b.types[29]++
			b.types = append(b.types, b.name("foreign"), 1, 8*8)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			b := exitLANBTFTestFixture(binary.LittleEndian)
			mutate(b)
			if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err == nil {
				t.Fatal("unsafe layout accepted")
			}
		})
	}
}

func TestExitLANBTFTraversalBoundsAndQualifiedPointer(t *testing.T) {
	b := exitLANBTFTestFixture(binary.LittleEndian)
	qualified := b.add("annotation", 18, 0, 6)
	b.types[32] = qualified
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 65; i++ {
		qualified = b.add("alias", 8, 0, qualified)
	}
	b.types[32] = qualified
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err == nil {
		t.Fatal("unbounded qualifier chain accepted")
	}
	b = exitLANBTFTestFixture(binary.LittleEndian)
	array := b.add("", 3, 0, 0, 8, 1, 1) // Self-reference: no recursive size exists.
	b.types[29]++
	// Insert sibling in the context record before the array record.
	sibling := []uint32{b.name("cycle"), array, 0}
	b.types = append(b.types[:34], append(sibling, b.types[34:]...)...)
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err == nil {
		t.Fatal("array size cycle accepted")
	}
}

func TestExitLANBTFUnnamedPaddingBitfield(t *testing.T) {
	for _, offset := range []uint32{1, 32} {
		b := &exitLANBTFTestBuilder{order: binary.LittleEndian}
		b.add("unsigned int", 1, 0, 4, 32)
		b.add("sk_buff", 4, 2, 8, 0, 1, 3<<24|offset, b.name("mark"), 1, 32)
		b.types[5] |= 1 << 31
		b.add("", 2, 0, 2)
		b.add("bpf_nf_ctx", 4, 1, 8, b.name("skb"), 3, 0)
		ctx, mark, err := exitLANBTFMarkLayout(b.raw(), 8)
		if offset == 1 {
			if err != nil || ctx != 0 || mark != 4 {
				t.Fatalf("nonoverlapping padding: %d %d %v", ctx, mark, err)
			}
		} else if err == nil {
			t.Fatal("padding overlapping mark accepted")
		}
	}
}

func TestExitLANBTFHeaderAndBounds(t *testing.T) {
	raw := exitLANBTFTestFixture(binary.LittleEndian).raw()
	for end := 0; end < len(raw); end++ {
		if _, _, err := exitLANBTFMarkLayout(raw[:end], 8); err == nil {
			t.Fatalf("truncation %d accepted", end)
		}
	}
	for _, pointer := range []uint32{0, 1, 16, 0xffffffff} {
		if _, _, err := exitLANBTFMarkLayout(raw, pointer); err == nil {
			t.Fatalf("pointer width %d accepted", pointer)
		}
	}
	for name, mutate := range map[string]func([]byte){
		"magic":                func(r []byte) { r[0] = 0 },
		"version":              func(r []byte) { r[2] = 2 },
		"flags":                func(r []byte) { r[3] = 1 },
		"extension":            func(r []byte) { binary.LittleEndian.PutUint32(r[4:], 32) },
		"overlap":              func(r []byte) { binary.LittleEndian.PutUint32(r[16:], 0) },
		"offset overflow":      func(r []byte) { binary.LittleEndian.PutUint32(r[8:], 0xfffffffc) },
		"length overflow":      func(r []byte) { binary.LittleEndian.PutUint32(r[12:], 0xfffffffc) },
		"unterminated strings": func(r []byte) { r[len(r)-1] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := append([]byte(nil), raw...)
			mutate(r)
			if _, _, err := exitLANBTFMarkLayout(r, 8); err == nil {
				t.Fatal("malformed header accepted")
			}
		})
	}
	if _, _, err := exitLANBTFMarkLayout(make([]byte, exitLANBTFMaxBytes+1), 8); err == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestExitLANBTFSkipsKnownRecordsAndPromotesOnlyAnonymous(t *testing.T) {
	b := exitLANBTFTestFixture(binary.BigEndian)
	b.add("sk_buff", 7, 0, 0) // A forward declaration is not a second concrete struct.
	b.add("", 3, 0, 0, 1, 1, 2)
	b.add("enum", 6, 1, 4, b.name("value"), 1)
	b.add("", 9, 0, 1)
	b.add("", 11, 0, 1)
	b.add("function", 12, 1, 14)
	b.add("", 13, 1, 1, b.name("arg"), 1)
	b.add("variable", 14, 0, 1, 1)
	b.add("section", 15, 1, 4, 15, 0, 4)
	b.add("float", 16, 0, 4)
	b.add("tag", 17, 0, 4, 0xffffffff)
	b.add("annotation", 18, 0, 1)
	b.add("large_enum", 19, 1, 8, b.name("large"), 0, 1)
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err != nil {
		t.Fatal(err)
	}
	b.types[19] = b.name("named_container")
	if _, _, err := exitLANBTFMarkLayout(b.raw(), 8); err == nil {
		t.Fatal("named nested mark was promoted")
	}
}
