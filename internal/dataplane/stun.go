package dataplane

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"net/netip"
)

// The transaction belongs to a single UDP socket. Only the configured server
// may complete it. A successful Binding is a candidate, never a path proof.
type stunTransaction struct {
	server netip.AddrPort
	id     [12]byte
}

func newSTUNTransaction(server netip.AddrPort) (stunTransaction, [20]byte, error) {
	t := stunTransaction{server: server}
	var request [20]byte
	if _, err := rand.Read(t.id[:]); err != nil {
		return t, request, err
	}
	binary.BigEndian.PutUint16(request[:2], 1)
	binary.BigEndian.PutUint32(request[4:8], 0x2112a442)
	copy(request[8:], t.id[:])
	return t, request, nil
}

func (t stunTransaction) mapped(sender netip.AddrPort, raw []byte) (netip.AddrPort, bool) {
	var mapped netip.AddrPort
	if sender != t.server || len(raw) < 20 || len(raw) > 1500 || binary.BigEndian.Uint16(raw[:2]) != 0x0101 ||
		int(binary.BigEndian.Uint16(raw[2:4])) != len(raw)-20 || binary.BigEndian.Uint32(raw[4:8]) != 0x2112a442 || !bytes.Equal(raw[8:20], t.id[:]) {
		return netip.AddrPort{}, false
	}
	for attrs := raw[20:]; len(attrs) > 0; {
		if len(attrs) < 4 {
			return netip.AddrPort{}, false
		}
		kind := binary.BigEndian.Uint16(attrs)
		size := int(binary.BigEndian.Uint16(attrs[2:4]))
		padded := (size + 3) &^ 3
		if padded > len(attrs)-4 {
			return netip.AddrPort{}, false
		}
		value := attrs[4 : 4+size]
		attrs = attrs[4+padded:]
		if kind != 0x0020 {
			if kind < 0x8000 {
				return netip.AddrPort{}, false
			}
			continue
		}
		if mapped.IsValid() || size != 8 || value[1] != 1 {
			return netip.AddrPort{}, false
		}
		port := binary.BigEndian.Uint16(value[2:4]) ^ 0x2112
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], binary.BigEndian.Uint32(value[4:])^0x2112a442)
		addr := netip.AddrFrom4(ip)
		if port == 0 || !addr.IsGlobalUnicast() {
			return netip.AddrPort{}, false
		}
		mapped = netip.AddrPortFrom(addr, port)
	}
	return mapped, mapped.IsValid()
}
