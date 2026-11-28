package dataplane

import (
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// One reader owns this socket for its entire lifetime, including while it is
// reserved. Peer datagrams arriving without an engine reader are discarded.
type natSocket struct {
	*net.UDPConn
	bind                      atomic.Pointer[candidateBind]
	local                     netip.AddrPort
	mu                        sync.Mutex
	server                    netip.AddrPort
	writeMu                   sync.Mutex
	writeDeadline             time.Time
	transaction               stunTransaction
	requested, observed, next time.Time
	public                    netip.AddrPort
	reads                     chan natRead
	stopped                   chan struct{}
}

type natRead struct {
	buffer []byte
	result chan natDatagram
}
type natDatagram struct {
	n    int
	from netip.AddrPort
}

func reserveNATSocket(server netip.AddrPort, port uint16) (*natSocket, error) {
	route, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(server))
	if err != nil {
		return nil, err
	}
	addr := route.LocalAddr().(*net.UDPAddr).AddrPort().Addr()
	_ = route.Close()
	return listenNATSocket(addr, port)
}

func listenNATSocket(addr netip.Addr, port uint16) (*natSocket, error) {
	c, err := openNATUDP(addr, port)
	if err != nil {
		return nil, err
	}
	return startNATSocket(addr, c), nil
}

func startNATSocket(addr netip.Addr, c *net.UDPConn) *natSocket {
	s := &natSocket{UDPConn: c, local: netip.AddrPortFrom(addr, c.LocalAddr().(*net.UDPAddr).AddrPort().Port()), reads: make(chan natRead), stopped: make(chan struct{})}
	go s.run()
	return s
}

func (s *natSocket) run() {
	defer close(s.stopped)
	var buf [65507]byte
	for {
		n, from, destination, err := s.readNAT(buf[:])
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.server == from {
			if destination == s.local.Addr() && time.Since(s.requested) < 3*time.Second {
				if ap, ok := s.transaction.mapped(from, buf[:n]); ok {
					s.public, s.observed, s.transaction = ap, time.Now(), stunTransaction{}
				}
			}
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		if !destination.IsValid() || destination != s.packetSource() {
			continue
		}
		select {
		case r := <-s.reads:
			count := copy(r.buffer, buf[:n])
			if count != n {
				count = 0
			}
			r.result <- natDatagram{count, from}
		default:
		}
	}
}

func (s *natSocket) ReadFromUDPAddrPort(buf []byte) (int, netip.AddrPort, error) {
	r := natRead{buf, make(chan natDatagram, 1)}
	select {
	case <-s.stopped:
		return 0, netip.AddrPort{}, net.ErrClosed
	case s.reads <- r:
	}
	result := <-r.result
	return result.n, result.from, nil
}

func (s *natSocket) mapping() netip.AddrPort {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.observed) >= 30*time.Second {
		return netip.AddrPort{}
	}
	return s.public
}

func (s *natSocket) discover(server netip.AddrPort) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if server != s.server {
		s.server = server
		s.transaction = stunTransaction{}
		s.public = netip.AddrPort{}
		s.next = time.Time{}
	}
	if !server.IsValid() {
		s.transaction = stunTransaction{}
		s.public = netip.AddrPort{}
		s.next = time.Time{}
		return
	}
	if now.Before(s.next) {
		return
	}
	transaction, request, err := newSTUNTransaction(server)
	if err != nil {
		return
	}
	s.transaction, s.requested, s.next = transaction, now, now.Add(15*time.Second)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.UDPConn.SetWriteDeadline(now.Add(100*time.Millisecond)) == nil {
		_, _ = s.writeNAT(request[:], server, s.local.Addr())
	}
}

func (s *natSocket) SetWriteDeadline(deadline time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writeDeadline = deadline
	return nil
}

func (s *natSocket) WriteToUDPAddrPort(data []byte, peer netip.AddrPort) (int, error) {
	source := s.packetSource()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.UDPConn.SetWriteDeadline(s.writeDeadline); err != nil {
		return 0, err
	}
	return s.writeNAT(data, peer, source)
}

// A kernel preparation may open the socket and then fall back to userspace.
// A replacement must measure its own mapping, even if the local port survives.
type natHandoff struct {
	mu   sync.Mutex
	idle *natSocket
	addr netip.AddrPort
}

func newNATHandoff(s *natSocket) *natHandoff { return &natHandoff{idle: s, addr: s.local} }

func (h *natHandoff) open(port uint16) (candidateSocket, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.idle != nil && (port == 0 || port == h.addr.Port()) {
		s := h.idle
		h.idle = nil
		return s, nil
	}
	if port == 0 {
		port = h.addr.Port()
	}
	s, err := listenNATSocket(h.addr.Addr(), port)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (h *natHandoff) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.idle != nil {
		_ = h.idle.Close()
		h.idle = nil
	}
}

func (s *natSocket) packetSource() netip.Addr {
	b := s.bind.Load()
	if b == nil {
		return s.local.Addr()
	}
	b.mu.Lock()
	local, err := netip.ParseAddrPort(b.assignment.localEndpoint)
	b.mu.Unlock()
	if err != nil {
		return netip.Addr{}
	}
	if local == s.mapping() {
		return s.local.Addr()
	}
	if local.Port() == s.local.Port() && (multiInterfaceNAT || local == s.local) {
		return local.Addr()
	}
	return netip.Addr{}
}
