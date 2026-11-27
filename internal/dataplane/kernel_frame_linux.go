package dataplane

import "encoding/binary"

// The private ingress filter appends the original UDP payload length after the
// authenticated IPv4 frame. Consume the marker before dispatching plaintext.
func splitKernelIPv4Frame(raw []byte) ([]byte, int, error) {
	const footerSize = 4
	if len(raw) < 20+footerSize || raw[0]>>4 != 4 {
		return nil, 0, ErrPacketIO
	}
	n := int(binary.BigEndian.Uint16(raw[2:4]))
	header := int(raw[0]&15) * 4
	if header < 20 || header > n || n != len(raw)-footerSize {
		return nil, 0, ErrPacketIO
	}
	if raw[n] != 'E' || raw[n+1] != 'N' {
		return nil, 0, ErrPacketIO
	}
	wire := int(binary.BigEndian.Uint16(raw[n+2:]))
	if wire < n+32 || wire > 65507 {
		return nil, 0, ErrPacketIO
	}
	return raw[:n:n], wire, nil
}
