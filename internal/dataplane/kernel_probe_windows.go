package dataplane

import (
	"errors"
	"math/bits"
	"net/netip"
	"unsafe"

	"github.com/endless-net/client/api/transport"
	wg "github.com/tailscale/wireguard-go/device"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type windowsProbeSocket struct {
	handle      windows.Handle
	local, peer netip.Addr
	index       uint32
	luid        winipcfg.LUID
}

func openWindowsProbeSocket(local, peer netip.Addr, index uint32, luid winipcfg.LUID) (*windowsProbeSocket, error) {
	if index == 0 || luid == 0 || !hostIPv4(local) || !hostIPv4(peer) || local == peer {
		return nil, ErrConfiguration
	}
	h, err := windows.Socket(windows.AF_INET, windows.SOCK_RAW, windows.IPPROTO_UDP)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*windowsProbeSocket, error) { return nil, errors.Join(err, windows.Closesocket(h)) }
	// Winsock expects the IPv4 unicast interface index in network byte order.
	// Bind outbound probes to the owned adapter even if the OS changes routes.
	const ipUnicastIf = 31
	if err := windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, int(bits.ReverseBytes32(index))); err != nil {
		return fail(err)
	}
	if err := windows.Bind(h, &windows.SockaddrInet4{Addr: local.As4()}); err != nil {
		return fail(err)
	}
	for _, option := range []int{windows.IP_HDRINCL, windows.IP_PKTINFO} {
		if err := windows.SetsockoptInt(h, windows.IPPROTO_IP, option, 1); err != nil {
			return fail(err)
		}
	}
	if err := windows.SetsockoptInt(h, windows.SOL_SOCKET, windows.SO_RCVTIMEO, 2000); err != nil {
		return fail(err)
	}
	return &windowsProbeSocket{handle: h, local: local, peer: peer, index: index, luid: luid}, nil
}

// WireGuardNT 1.1 CalculateNblPadding clamps the last padding block to
// the IPv4 interface MTU. Control packets carry DF and must fit in one NBL.
func windowsProbeCiphertextSize(length, mtu int) (int, error) {
	if length < 28 || length > 28+transport.MaxDatagramBytes || length > mtu {
		return 0, ErrPacketIO
	}
	padded := (length + wg.PaddingMultiple - 1) &^ (wg.PaddingMultiple - 1)
	return min(padded, mtu) + wg.MessageTransportSize, nil
}

func (s *windowsProbeSocket) send(raw []byte, reserve func(int) error) error {
	if _, claimed, err := decodeControlPacket(raw, s.local, s.peer); !claimed || err != nil {
		return ErrNoAuthority
	}
	if reserve != nil {
		row, err := s.luid.IPInterface(windows.AF_INET)
		if err != nil {
			return err
		}
		if row.InterfaceLUID != s.luid || row.InterfaceIndex != s.index {
			return ErrAdapterOwnership
		}
		size, err := windowsProbeCiphertextSize(len(raw), int(row.NLMTU))
		if err != nil {
			return err
		}
		if err := reserve(size); err != nil {
			return err
		}
	}
	return windows.Sendto(s.handle, raw, 0, &windows.SockaddrInet4{Addr: s.peer.As4()})
}

func (s *windowsProbeSocket) receive(raw []byte) (int, error) {
	if len(raw) < 28+transport.MaxDatagramBytes {
		return 0, ErrPacketIO
	}
	var info struct {
		header windows.WSACMSGHDR
		packet windows.IN_PKTINFO
	}
	buf := windows.WSABuf{Buf: &raw[0], Len: 28 + transport.MaxDatagramBytes}
	msg := windows.WSAMsg{Buffers: &buf, BufferCount: 1, Control: windows.WSABuf{Buf: (*byte)(unsafe.Pointer(&info)), Len: uint32(unsafe.Sizeof(info))}}
	var n uint32
	if err := windows.WSARecvMsg(s.handle, &msg, &n, nil, nil); err != nil {
		return 0, err
	}
	if msg.Flags != 0 || msg.Control.Len != uint32(unsafe.Sizeof(info)) || info.header.Len != unsafe.Sizeof(info) ||
		info.header.Level != windows.IPPROTO_IP || info.header.Type != windows.IP_PKTINFO ||
		info.packet.Ifindex != s.index || info.packet.Addr != s.local.As4() || n > buf.Len {
		return 0, ErrNoAuthority
	}
	if _, claimed, err := decodeControlPacket(raw[:n], s.peer, s.local); !claimed || err != nil {
		return 0, ErrNoAuthority
	}
	return int(n), nil
}

func (s *windowsProbeSocket) Close() error { return windows.Closesocket(s.handle) }
