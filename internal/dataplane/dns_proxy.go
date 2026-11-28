//go:build linux || windows

package dataplane

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"golang.org/x/net/dns/dnsmessage"
)

// The OS resolver sends managed names to this session-owned loopback socket.
type sessionDNSProxy struct {
	ctx     context.Context
	cancel  context.CancelFunc
	addr    netip.AddrPort
	udp     *net.UDPConn
	tcp     *net.TCPListener
	routes  connectivity.DNSRoutes
	timeout time.Duration
	active  func() bool
	wg      sync.WaitGroup
}

func openSessionDNSProxy(parent context.Context, bind netip.AddrPort, listener net.ListenConfig, routes connectivity.DNSRoutes, timeout time.Duration, active func() bool) (*sessionDNSProxy, error) {
	if !bind.Addr().Is4() || !bind.Addr().IsLoopback() || timeout <= 0 || active == nil {
		return nil, ErrConfiguration
	}
	tcpListener, err := listener.Listen(parent, "tcp4", bind.String())
	if err != nil {
		return nil, err
	}
	tcp := tcpListener.(*net.TCPListener)
	addr := tcp.Addr().(*net.TCPAddr).AddrPort()
	packet, err := listener.ListenPacket(parent, "udp4", addr.String())
	if err != nil {
		_ = tcp.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	p := &sessionDNSProxy{ctx: ctx, cancel: cancel, addr: addr, tcp: tcp, udp: packet.(*net.UDPConn), routes: routes, timeout: timeout, active: active}
	p.wg.Add(2)
	go p.serveUDP()
	go p.serveTCP()
	return p, nil
}

func (p *sessionDNSProxy) close() {
	p.cancel()
	_ = p.udp.Close()
	_ = p.tcp.Close()
	p.wg.Wait()
}

// Recheck permission at each upstream attempt and write. Losing the source
// or lease while a server is silent must not authorize the next retry.
func (p *sessionDNSProxy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !p.active() || p.ctx.Err() != nil {
		return nil, ErrNoAuthority
	}
	if address == p.addr.String() {
		return nil, connectivity.ErrDNSUpstream
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return dnsUpstreamConn{Conn: conn, proxy: p}, nil
}

type dnsUpstreamConn struct {
	net.Conn
	proxy *sessionDNSProxy
}

func (c dnsUpstreamConn) Write(packet []byte) (int, error) {
	if !c.proxy.active() || c.proxy.ctx.Err() != nil {
		return 0, ErrNoAuthority
	}
	return c.Conn.Write(packet)
}

func (p *sessionDNSProxy) reply(packet []byte) []byte {
	if p.active() && p.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(p.ctx, p.timeout)
		response, err := p.routes.Exchange(ctx, packet, p)
		cancel()
		if err == nil && p.active() && p.ctx.Err() == nil {
			return response
		}
	}
	var parser dnsmessage.Parser
	h, err := parser.Start(packet)
	if err != nil || h.Response {
		return nil
	}
	q, err := parser.Question()
	if err != nil {
		return nil
	}
	failure := dnsmessage.Message{Header: dnsmessage.Header{ID: h.ID, Response: true, RecursionDesired: h.RecursionDesired, RecursionAvailable: true, RCode: dnsmessage.RCodeServerFailure}, Questions: []dnsmessage.Question{q}}
	wire, _ := failure.Pack()
	return wire
}
func (p *sessionDNSProxy) serveUDP() {
	defer p.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, from, err := p.udp.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		if !from.Addr().IsLoopback() {
			continue
		}
		if reply := p.reply(buf[:n]); reply != nil {
			if datagram := dnsUDPReply(buf[:n], reply); len(datagram) > 0 {
				_, _ = p.udp.WriteToUDPAddrPort(datagram, from)
			}
		}
	}
}

// Honor the stub caller's UDP size. A larger upstream TCP answer is
// available when the caller repeats its question over TCP.
func dnsUDPReply(query, reply []byte) []byte {
	if len(reply) <= 512 {
		return reply
	}
	var p dnsmessage.Parser
	_, err := p.Start(query)
	if err != nil {
		return nil
	}
	q, err := p.Question()
	if err != nil || p.SkipAllQuestions() != nil || p.SkipAllAnswers() != nil || p.SkipAllAuthorities() != nil {
		return nil
	}
	limit := 512
	for {
		h, err := p.AdditionalHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil
		}
		if h.Type == dnsmessage.TypeOPT {
			limit = max(limit, int(h.Class))
		}
		if p.SkipAdditional() != nil {
			return nil
		}
	}
	// The loopback listener is UDP/IPv4. EDNS may advertise more than
	// fits after the IPv4 and UDP headers, even on a local connection.
	const maxIPv4UDPPayload = 65535 - 20 - 8
	if len(reply) <= min(limit, maxIPv4UDPPayload) {
		return reply
	}
	head, err := p.Start(reply)
	if err != nil {
		return nil
	}
	head.Truncated = true
	m := dnsmessage.Message{Header: head, Questions: []dnsmessage.Question{q}}
	wire, _ := m.Pack()
	return wire
}

func (p *sessionDNSProxy) serveTCP() {
	defer p.wg.Done()
	for {
		c, err := p.tcp.AcceptTCP()
		if err != nil {
			return
		}
		if c.RemoteAddr().(*net.TCPAddr).AddrPort().Addr().IsLoopback() {
			stop := context.AfterFunc(p.ctx, func() { _ = c.Close() })
			if c.SetDeadline(time.Now().Add(p.timeout)) == nil {
				p.tcpQuery(c)
			}
			stop()
		}
		_ = c.Close()
	}
}
func (p *sessionDNSProxy) tcpQuery(c *net.TCPConn) {
	var length [2]byte
	if _, err := io.ReadFull(c, length[:]); err != nil {
		return
	}
	packet := make([]byte, binary.BigEndian.Uint16(length[:]))
	if _, err := io.ReadFull(c, packet); err != nil {
		return
	}
	reply := p.reply(packet)
	if reply == nil {
		return
	}
	binary.BigEndian.PutUint16(length[:], uint16(len(reply)))
	_, _ = c.Write(append(length[:], reply...))
}
