package dataplane

import (
	"bytes"
	"encoding/binary"

	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"golang.org/x/crypto/chacha20"

	"github.com/endless-net/client/api/transport"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
)

// Device attributes from amneziawg-linux-kernel-module v3.1.20261113,
// src/uapi/wireguard.h. S3 and S4 were added after the original header fields.
const (
	awgJunkCount      = 9
	awgJunkMin        = 10
	awgJunkMax        = 11
	awgPadding1       = 12
	awgPadding2       = 13
	awgHeader1        = 14
	awgPadding3       = 19
	awgPadding4       = 20
	awgHeaderKey      = 26
	awgContentPadding = 27
	awgRandomTrailers = 33
	awgDisableCookies = 34
)

// The caller validates the signed profile before encoding. Range low bounds
// occupy the low half-word. The candidate bind owns the declarative prelude.
func kernelAWGSettings(profile *transport.AWG) []netlink.Attribute {
	flag := func(on bool) []byte {
		if on {
			return []byte{1}
		}
		return []byte{0}
	}
	settings := []netlink.Attribute{
		{Type: awgJunkCount, Data: nlenc.Uint16Bytes(0)},
		{Type: awgJunkMin, Data: nlenc.Uint16Bytes(0)},
		{Type: awgJunkMax, Data: nlenc.Uint16Bytes(0)},
		{Type: awgHeaderKey, Data: profile.HeaderProtectionKey},
		{Type: awgContentPadding, Data: nlenc.Uint32Bytes(profile.ContentPadding.Max<<16 | profile.ContentPadding.Min)},
		{Type: awgRandomTrailers, Data: flag(profile.RandomTrailers)},
		{Type: awgDisableCookies, Data: flag(profile.DisableCookies)},
	}
	for i, typ := range [...]uint16{awgPadding1, awgPadding2, awgPadding3, awgPadding4} {
		r := profile.Headers[i]
		settings = append(settings,
			netlink.Attribute{Type: typ, Data: nlenc.Uint16Bytes(profile.Padding[i])},
			netlink.Attribute{Type: awgHeader1 + uint16(i), Data: nlenc.Uint64Bytes(uint64(r.Max)<<32 | uint64(r.Min))},
		)
	}
	return settings
}

// A successful SET alone cannot distinguish a driver that silently ignores
// an unknown extension. Match every required field, including false and zero.
func verifyKernelAWGSettings(messages []genetlink.Message, expected []netlink.Attribute) error {
	want := make(map[uint16][]byte, len(expected))
	for _, attr := range expected {
		want[attr.Type] = attr.Data
	}
	seen := make(map[uint16]bool, len(want))
	for _, message := range messages {
		fields, err := netlink.NewAttributeDecoder(message.Data)
		if err != nil {
			return err
		}
		for fields.Next() {
			typ := fields.Type()
			value, required := want[typ]
			if !required {
				continue
			}
			if seen[typ] {
				return ErrConfiguration
			}
			seen[typ] = true
			fields.Do(func(data []byte) error {
				if !bytes.Equal(data, value) {
					return ErrConfiguration
				}
				return nil
			})
		}
		if err := fields.Err(); err != nil {
			return err
		}
	}
	if len(seen) != len(want) {
		return ErrConfiguration
	}
	return nil
}

// Match the two protected handshake headers produced by the configured kernel
// for candidateBind's bounded retransmit cache (AWG 3.1).
func kernelAWGHandshake(profile *transport.AWG, raw []byte) bool {
	if len(raw) < awgdevice.HeaderCipherNonceSize {
		return false
	}
	var mask [4]byte
	if !bytes.Equal(profile.HeaderProtectionKey, make([]byte, 32)) {
		c, err := chacha20.NewUnauthenticatedCipher(profile.HeaderProtectionKey, raw[:awgdevice.HeaderCipherNonceSize])
		if err != nil {
			return false
		}
		c.XORKeyStream(mask[:], mask[:])
	}
	for i, base := range [...]int{awgdevice.MessageInitiationSize, awgdevice.MessageResponseSize} {
		offset := int(profile.Padding[i])
		expected := offset + base
		if len(raw) < expected || !profile.RandomTrailers && len(raw) != expected {
			continue
		}
		value := binary.LittleEndian.Uint32(raw[offset:]) ^ binary.LittleEndian.Uint32(mask[:])
		r := profile.Headers[i]
		if value >= r.Min && value <= r.Max {
			return true
		}
	}
	return false
}
