package dataplane

import (
	"net"
	"net/netip"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A wildcard listener keeps its advertised port as candidates change. Winsock
// packet info selects the source on send and identifies the actual LAN on receive.
type windowsLANCandidate struct {
	*net.UDPConn
	bind    *candidateBind
	mu      sync.Mutex
	address netip.Addr
	index   uint32
	checked time.Time
}

type candidatePacketInfo struct {
	header windows.WSACMSGHDR
	packet windows.IN_PKTINFO
}

func (info *candidatePacketInfo) bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(info)), unsafe.Sizeof(*info))
}

func (b *candidateBind) listenUDP(port uint16) (candidateSocket, error) {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
	if err != nil {
		return nil, err
	}
	raw, err := c.SyscallConn()
	if err == nil {
		var optErr error
		err = raw.Control(func(fd uintptr) {
			optErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_PKTINFO, 1)
		})
		if err == nil {
			err = optErr
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return &windowsLANCandidate{UDPConn: c, bind: b}, nil
}

func (s *windowsLANCandidate) source() (windows.IN_PKTINFO, bool) {
	s.bind.mu.Lock()
	ep := s.bind.assignment.localEndpoint
	s.bind.mu.Unlock()
	ap, err := netip.ParseAddrPort(ep)
	if err != nil || !ap.Addr().Is4() {
		return windows.IN_PKTINFO{}, false
	}
	addr := ap.Addr()
	s.mu.Lock()
	defer s.mu.Unlock()
	if addr != s.address || time.Since(s.checked) >= 3*time.Second {
		s.checked = time.Now()
		s.address, s.index = addr, 0
		rows, err := LANIPv4Interfaces()
		if err != nil {
			return windows.IN_PKTINFO{}, false
		}
		s.index = rows[addr]
	}
	return windows.IN_PKTINFO{Addr: addr.As4(), Ifindex: s.index}, s.index != 0
}

func (s *windowsLANCandidate) WriteToUDPAddrPort(buf []byte, peer netip.AddrPort) (int, error) {
	packet, ok := s.source()
	if !ok {
		return 0, ErrNoAuthority
	}
	info := candidatePacketInfo{packet: packet}
	info.header = windows.WSACMSGHDR{Len: unsafe.Sizeof(info), Level: windows.IPPROTO_IP, Type: windows.IP_PKTINFO}
	n, _, err := s.WriteMsgUDPAddrPort(buf, info.bytes(), peer)
	return n, err
}

func (s *windowsLANCandidate) ReadFromUDPAddrPort(buf []byte) (int, netip.AddrPort, error) {
	for {
		var info candidatePacketInfo
		n, control, flags, peer, err := s.ReadMsgUDPAddrPort(buf, info.bytes())
		if err != nil {
			return 0, netip.AddrPort{}, err
		}
		packet, ok := s.source()
		if ok && flags == 0 && control == len(info.bytes()) && info.header.Len == unsafe.Sizeof(info) && info.header.Level == windows.IPPROTO_IP && info.header.Type == windows.IP_PKTINFO && info.packet == packet {
			return n, peer, nil
		}
	}
}
