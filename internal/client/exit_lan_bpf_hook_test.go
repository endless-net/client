package client

import (
	"encoding/binary"
	"testing"
)

func exitLANHookTestAttr(order binary.ByteOrder, kind uint16, value []byte) []byte {
	size := 4 + len(value)
	raw := make([]byte, (size+3)&^3)
	order.PutUint16(raw, uint16(size))
	order.PutUint16(raw[2:], kind)
	copy(raw[4:], value)
	return raw
}
func exitLANHookTestNumber(value uint32) []byte {
	raw := make([]byte, 4)
	binary.BigEndian.PutUint32(raw, value)
	return raw
}
func exitLANHookTestRecord(order binary.ByteOrder, seq uint32, identity exitLANBPFLinkIdentity) []byte {
	body := []byte{byte(identity.family), 0, 0, 0}
	body = append(body, exitLANHookTestAttr(order, 1, exitLANHookTestNumber(identity.hook))...)
	body = append(body, exitLANHookTestAttr(order, 2, exitLANHookTestNumber(uint32(identity.priority)))...)
	info := exitLANHookTestAttr(order, 2, exitLANHookTestNumber(2))
	info = append(info, exitLANHookTestAttr(order, 0x8001, exitLANHookTestAttr(order, 1, exitLANHookTestNumber(identity.programID)))...)
	body = append(body, exitLANHookTestAttr(order, 0x8006, info)...)
	raw := make([]byte, 16)
	order.PutUint32(raw, uint32(16+len(body)))
	order.PutUint16(raw[4:], exitLANHookMessage)
	order.PutUint16(raw[6:], 2)
	order.PutUint32(raw[8:], seq)
	return append(raw, body...)
}
func exitLANHookTestDone(order binary.ByteOrder, seq, port uint32) []byte {
	raw := make([]byte, 20)
	order.PutUint32(raw, 20)
	order.PutUint16(raw[4:], 3)
	order.PutUint16(raw[6:], 2)
	order.PutUint32(raw[8:], seq)
	order.PutUint32(raw[12:], port)
	return raw
}

func TestExitLANHookDumpExactIdentityAndCompletion(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, family := range []uint32{2, 10} {
			expected := exitLANBPFLinkIdentity{1, 101, family, 4, -100}
			d, err := newExitLANHookDump(order, 7, 49, expected)
			if err != nil {
				t.Fatal(err)
			}
			request := d.request()
			if len(request) != 28 || order.Uint16(request[4:]) != 0xc00 || order.Uint16(request[6:]) != 0x301 || order.Uint32(request[8:]) != 7 || order.Uint32(request[12:]) != 49 || request[16] != byte(family) || binary.BigEndian.Uint32(request[24:]) != 4 {
				t.Fatal("bad hook request")
			}
			other := expected
			other.programID = 102
			other.priority = 100
			data := append(exitLANHookTestRecord(order, 7, other), exitLANHookTestRecord(order, 7, expected)...)
			ordinary := append([]byte(nil), exitLANHookTestRecord(order, 7, expected)[:36]...)
			ordinary = append(ordinary, exitLANHookTestAttr(order, 4, []byte("ordinary_hook\x00"))...)
			ordinary = append(ordinary, exitLANHookTestAttr(order, 5, []byte("test_module\x00"))...)
			order.PutUint32(ordinary, uint32(len(ordinary)))
			data = append(data, ordinary...)
			desc := exitLANHookTestAttr(order, 1, []byte("owned_table\x00"))
			desc = append(desc, exitLANHookTestAttr(order, 3, []byte("output\x00"))...)
			desc = append(desc, exitLANHookTestAttr(order, 2, []byte{1})...)
			info := exitLANHookTestAttr(order, 2, exitLANHookTestNumber(1))
			info = append(info, exitLANHookTestAttr(order, 0x8001, desc)...)
			nft := append(ordinary, exitLANHookTestAttr(order, 0x8006, info)...)
			order.PutUint32(nft, uint32(len(nft)))
			data = append(data, nft...)
			if err := d.consume(data, true, false); err != nil || d.confirmed() {
				t.Fatal("data confirmed without DONE", err)
			}
			if err := d.consume(exitLANHookTestDone(order, 7, 49), true, false); err != nil || !d.confirmed() {
				t.Fatal("exact dump not confirmed", err)
			}
		}
	}
}

func TestExitLANHookDumpRejectsIncompleteAndDuplicateEvidence(t *testing.T) {
	expected := exitLANBPFLinkIdentity{1, 101, 2, 4, 10}
	for _, scenario := range []string{"empty", "other_program", "non_bpf", "duplicate", "trailing", "ACK", "error_done", "interrupted_done", "wrong_done_port"} {
		d, _ := newExitLANHookDump(binary.LittleEndian, 7, 49, expected)
		row := exitLANHookTestRecord(d.order, 7, expected)
		done := exitLANHookTestDone(d.order, 7, 49)
		switch scenario {
		case "empty":
			row = nil
		case "other_program":
			binary.BigEndian.PutUint32(row[56:], 102)
		case "non_bpf":
			binary.BigEndian.PutUint32(row[44:], 1)
		case "duplicate":
			row = append(row, row...)
		case "trailing":
			done = append(done, row...)
		case "ACK":
			d.order.PutUint16(done[4:], 2)
		case "error_done":
			d.order.PutUint32(done[16:], 1)
		case "interrupted_done":
			d.order.PutUint16(done[6:], 0x12)
		case "wrong_done_port":
			d.order.PutUint32(done[12:], 0)
		}
		_ = d.consume(append(row, done...), true, false)
		if d.confirmed() {
			t.Fatal("invalid dump confirmed", scenario)
		}
	}
}

func TestExitLANHookDumpRejectsMalformedAndLostData(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		expected := exitLANBPFLinkIdentity{1, 101, 2, 4, -100}
		for name, mutate := range map[string]func([]byte){
			"short":               func(b []byte) { order.PutUint32(b, 15) },
			"long":                func(b []byte) { order.PutUint32(b, 61) },
			"type":                func(b []byte) { order.PutUint16(b[4:], 2) },
			"sequence":            func(b []byte) { order.PutUint32(b[8:], 8) },
			"sender_header":       func(b []byte) { order.PutUint32(b[12:], 49) },
			"missing_multi":       func(b []byte) { order.PutUint16(b[6:], 0) },
			"interrupted":         func(b []byte) { order.PutUint16(b[6:], 0x12) },
			"family":              func(b []byte) { b[16] = 10 },
			"version":             func(b []byte) { b[17] = 1 },
			"res_id":              func(b []byte) { b[19] = 1 },
			"hook":                func(b []byte) { binary.BigEndian.PutUint32(b[24:], 3) },
			"priority":            func(b []byte) { binary.BigEndian.PutUint32(b[32:], 0) },
			"missing_hook":        func(b []byte) { order.PutUint16(b[22:], 9) },
			"duplicate_hook":      func(b []byte) { order.PutUint16(b[30:], 1) },
			"byteorder_flag":      func(b []byte) { order.PutUint16(b[22:], 0x4001) },
			"short_number":        func(b []byte) { order.PutUint16(b[20:], 7) },
			"missing_nested":      func(b []byte) { order.PutUint16(b[38:], 6) },
			"missing_desc_nested": func(b []byte) { order.PutUint16(b[50:], 1) },
			"zero_program":        func(b []byte) { binary.BigEndian.PutUint32(b[56:], 0) },
			"short_program":       func(b []byte) { order.PutUint16(b[52:], 7) },
		} {
			d, _ := newExitLANHookDump(order, 7, 49, expected)
			raw := exitLANHookTestRecord(order, 7, expected)
			mutate(raw)
			if err := d.consume(raw, true, false); err == nil {
				t.Fatal("malformed dump accepted", name, order)
			}
			if err := d.consume(append(exitLANHookTestRecord(order, 7, expected), exitLANHookTestDone(order, 7, 49)...), true, false); err == nil || d.confirmed() {
				t.Fatal("failed dump revived")
			}
		}
		for _, flags := range [][2]bool{{false, false}, {true, true}} {
			d, _ := newExitLANHookDump(order, 7, 49, expected)
			if err := d.consume(exitLANHookTestRecord(order, 7, expected), flags[0], flags[1]); err == nil {
				t.Fatal("sender/loss ignored")
			}
		}
	}
}

func TestExitLANHookDumpWorkBounds(t *testing.T) {
	expected := exitLANBPFLinkIdentity{1, 101, 2, 4, 10}
	for _, which := range []string{"datagram", "bytes", "messages"} {
		d, _ := newExitLANHookDump(binary.LittleEndian, 7, 49, expected)
		raw := exitLANHookTestRecord(d.order, 7, expected)
		switch which {
		case "datagram":
			raw = make([]byte, 65537)
		case "bytes":
			d.bytes = 1 << 20
		case "messages":
			d.messages = 8192
		}
		if d.consume(raw, true, false) == nil {
			t.Fatal("work bound ignored", which)
		}
	}
	var attrs []byte
	for i := 1; i <= 33; i++ {
		attrs = append(attrs, exitLANHookTestAttr(binary.LittleEndian, uint16(i), nil)...)
	}
	if _, err := exitLANHookAttributes(attrs, binary.LittleEndian); err == nil {
		t.Fatal("attribute count unbounded")
	}
	if _, err := newExitLANHookDump(nil, 7, 49, expected); err == nil {
		t.Fatal("unspecified order accepted")
	}
}
