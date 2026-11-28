package dataplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/netip"
	"slices"

	"github.com/endless-net/client/api/transport"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// configureKernelWGPeer installs one verified pair on a newly created device
// in its private network namespace. Its only UDP endpoint is the local relay
// socket. The candidate bind owns the public endpoint and the send policy.
func configureKernelWGPeer(ctx context.Context, conn *genetlink.Conn, index int, localSocket netip.AddrPort, cfg directCipherConfig) error {
	a := cfg.assignment
	if index <= 0 || conn == nil || cfg.clock == nil || !localSocket.Addr().Is4() || !localSocket.Addr().IsLoopback() || localSocket.Port() == 0 {
		return ErrConfiguration
	}
	digest, err := transport.ProfileDigest(a.offer.Profile)
	if err != nil || digest != a.offer.Digest {
		return ErrConfiguration
	}
	public, err := peerPublicKey(a.local, a.peer, cfg.private)
	if err != nil {
		return err
	}
	peerIPs, err := a.directPeerIPs()
	if err != nil {
		return err
	}
	family, err := conn.GetFamily(a.offer.Profile.Engine)
	if err != nil {
		return err
	}
	encoder := netlink.NewAttributeEncoder()
	encoder.Uint32(unix.WGDEVICE_A_IFINDEX, uint32(index))
	encoder.Bytes(unix.WGDEVICE_A_PRIVATE_KEY, cfg.private)
	encoder.Uint32(unix.WGDEVICE_A_FLAGS, unix.WGDEVICE_F_REPLACE_PEERS)
	var settings []netlink.Attribute
	if a.offer.Profile.Engine == transport.AmneziaWG {
		settings = kernelAWGSettings(a.offer.Profile.AWG)
		for _, attr := range settings {
			encoder.Bytes(attr.Type, attr.Data)
		}
	}
	encoder.Nested(unix.WGDEVICE_A_PEERS, func(peers *netlink.AttributeEncoder) error {
		peers.Nested(0, func(peer *netlink.AttributeEncoder) error {
			peer.Bytes(unix.WGPEER_A_PUBLIC_KEY, public)
			peer.Uint32(unix.WGPEER_A_FLAGS, unix.WGPEER_F_REPLACE_ALLOWEDIPS)
			var address [unix.SizeofSockaddrInet4]byte
			binary.NativeEndian.PutUint16(address[:2], unix.AF_INET)
			binary.BigEndian.PutUint16(address[2:4], localSocket.Port())
			copy(address[4:8], localSocket.Addr().AsSlice())
			peer.Bytes(unix.WGPEER_A_ENDPOINT, address[:])
			peer.Nested(unix.WGPEER_A_ALLOWEDIPS, func(ips *netlink.AttributeEncoder) error {
				for i, peerIP := range peerIPs {
					ips.Nested(uint16(i), func(ip *netlink.AttributeEncoder) error {
						ip.Uint16(unix.WGALLOWEDIP_A_FAMILY, unix.AF_INET)
						ip.Bytes(unix.WGALLOWEDIP_A_IPADDR, peerIP.AsSlice())
						ip.Uint8(unix.WGALLOWEDIP_A_CIDR_MASK, 32)
						return nil
					})
				}
				return nil
			})
			return nil
		})
		return nil
	})
	data, err := encoder.Encode()
	if err != nil {
		return ErrConfiguration
	}
	defer clear(data)
	sample, err := cfg.clock.Sample()
	if err != nil || ctx.Err() != nil || a.permission(sample.Wall, true) != nil {
		return ErrNoAuthority
	}
	_, err = conn.Execute(genetlink.Message{Header: genetlink.Header{Command: unix.WG_CMD_SET_DEVICE, Version: 1}, Data: data}, family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return err
	}
	return readKernelDevice(conn, family.ID, index, func(messages []genetlink.Message) error {
		if err := verifyLinuxPeer(messages, a.local.PublicKey, public, localSocket, peerIPs); err != nil {
			return err
		}
		if len(settings) != 0 {
			return verifyKernelAWGSettings(messages, settings)
		}
		return nil
	})
}

// Each candidate owns one peer with at most two IPv4 host prefixes. This fits
// a single device dump record. Check its effective configuration before UP.
func verifyLinuxPeer(messages []genetlink.Message, localKey string, public []byte, endpoint netip.AddrPort, addresses []netip.Addr) error {
	if len(messages) != 1 {
		return ErrConfiguration
	}
	fields, err := netlink.NewAttributeDecoder(messages[0].Data)
	if err != nil {
		return err
	}
	var local, peer bool
	for fields.Next() {
		switch fields.Type() {
		case unix.WGDEVICE_A_PUBLIC_KEY:
			if local || base64.StdEncoding.EncodeToString(fields.Bytes()) != localKey {
				return ErrConfiguration
			}
			local = true
		case unix.WGDEVICE_A_PEERS:
			fields.Nested(func(peers *netlink.AttributeDecoder) error {
				for peers.Next() {
					if peer {
						return ErrConfiguration
					}
					peer = true
					peers.Nested(func(attrs *netlink.AttributeDecoder) error {
						return verifyLinuxPeerAttrs(attrs, public, endpoint, addresses)
					})
				}
				return peers.Err()
			})
		}
	}
	if err := fields.Err(); err != nil {
		return err
	}
	if !local || !peer {
		return ErrConfiguration
	}
	return nil
}

func verifyLinuxPeerAttrs(attrs *netlink.AttributeDecoder, public []byte, endpoint netip.AddrPort, addresses []netip.Addr) error {
	var key, socket bool
	remaining := slices.Clone(addresses)
	for attrs.Next() {
		switch attrs.Type() {
		case unix.WGPEER_A_PUBLIC_KEY:
			if key || !bytes.Equal(attrs.Bytes(), public) {
				return ErrConfiguration
			}
			key = true
		case unix.WGPEER_A_ENDPOINT:
			raw := attrs.Bytes()
			if socket || len(raw) != unix.SizeofSockaddrInet4 || binary.NativeEndian.Uint16(raw[:2]) != unix.AF_INET ||
				binary.BigEndian.Uint16(raw[2:4]) != endpoint.Port() || !bytes.Equal(raw[4:8], endpoint.Addr().AsSlice()) {
				return ErrConfiguration
			}
			socket = true
		case unix.WGPEER_A_ALLOWEDIPS:
			attrs.Nested(func(ips *netlink.AttributeDecoder) error {
				for ips.Next() {
					ips.Nested(func(ip *netlink.AttributeDecoder) error {
						addr, err := kernelHostAddress(ip)
						if err != nil {
							return err
						}
						i := slices.Index(remaining, addr)
						if i < 0 {
							return ErrConfiguration
						}
						remaining = slices.Delete(remaining, i, i+1)
						return nil
					})
				}
				return ips.Err()
			})
		}
	}
	if err := attrs.Err(); err != nil {
		return err
	}
	if !key || !socket || len(remaining) != 0 {
		return ErrConfiguration
	}
	return nil
}

func kernelHostAddress(ip *netlink.AttributeDecoder) (netip.Addr, error) {
	var family uint16
	var mask uint8
	var addr netip.Addr
	for ip.Next() {
		switch ip.Type() {
		case unix.WGALLOWEDIP_A_FAMILY:
			family = ip.Uint16()
		case unix.WGALLOWEDIP_A_CIDR_MASK:
			mask = ip.Uint8()
		case unix.WGALLOWEDIP_A_IPADDR:
			addr, _ = netip.AddrFromSlice(ip.Bytes())
		}
	}
	if err := ip.Err(); err != nil {
		return netip.Addr{}, err
	}

	if family != unix.AF_INET || mask != 32 || !addr.Is4() {
		return netip.Addr{}, ErrConfiguration
	}
	return addr, nil
}

// kernelListenPort reads back the automatic UDP port after the owner brings
// the interface up. Discard the rest of the device dump, including its keys.
func kernelListenPort(conn *genetlink.Conn, family string, index int) (uint16, error) {
	f, err := conn.GetFamily(family)
	if err != nil {
		return 0, err
	}
	var port uint16
	err = readKernelDevice(conn, f.ID, index, func(messages []genetlink.Message) error {
		for _, message := range messages {
			fields, err := netlink.NewAttributeDecoder(message.Data)
			if err != nil {
				return err
			}
			for fields.Next() {
				if fields.Type() == unix.WGDEVICE_A_LISTEN_PORT {
					port = fields.Uint16()
				}
			}
			if err := fields.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if port == 0 {
		return 0, ErrEngine
	}
	return port, nil
}

func readKernelDevice(conn *genetlink.Conn, family uint16, index int, inspect func([]genetlink.Message) error) error {
	encoder := netlink.NewAttributeEncoder()
	encoder.Uint32(unix.WGDEVICE_A_IFINDEX, uint32(index))
	data, err := encoder.Encode()
	if err != nil {
		return err
	}
	messages, err := conn.Execute(genetlink.Message{Header: genetlink.Header{Command: unix.WG_CMD_GET_DEVICE, Version: 1}, Data: data}, family, netlink.Request|netlink.Dump)
	if err != nil {
		return err
	}
	for _, message := range messages {
		defer clear(message.Data)
	}
	return inspect(messages)
}
