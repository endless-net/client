package connectivity

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

var (
	ErrDNSUnmanaged = errors.New("no managed DNS route for this question")
	ErrDNSQuery     = errors.New("invalid DNS question")
	ErrDNSUpstream  = errors.New("assigned DNS servers unavailable")
)

type dnsDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

func dnsQuestion(packet []byte) (dnsmessage.Header, dnsmessage.Question, error) {
	var p dnsmessage.Parser
	h, err := p.Start(packet)
	if err != nil || h.OpCode != 0 || len(packet) > 65535 || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return h, dnsmessage.Question{}, ErrDNSQuery
	}
	q, err := p.Question()
	return h, q, err
}

// Exchange forwards one wire-format query under a caller-supplied deadline.
// Only ErrDNSUnmanaged permits handing the question to the system resolver.
func (r DNSRoutes) Exchange(ctx context.Context, packet []byte, dialer dnsDialer) ([]byte, error) {
	h, q, err := dnsQuestion(packet)
	if err != nil || h.Response {
		return nil, ErrDNSQuery
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	route := r.match(q.Name.String())
	if zone := r.local[route]; zone != nil || r.hasService(q.Name.String()) {
		return r.localReply(zone, h, q)
	}
	servers := r.zones[route]
	if len(servers) == 0 {
		return nil, ErrDNSUnmanaged
	}
	deadline, ok := ctx.Deadline()
	if !ok || dialer == nil {
		return nil, ErrConfig
	}
	for i, ip := range servers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Leave time for the next assigned server when a preferred one is silent.
		budget := time.Until(deadline) / time.Duration(len(servers)-i)
		attempt, cancel := context.WithTimeout(ctx, budget)
		response, err := dnsExchange(attempt, dialer, ip, "udp", packet, h, q)
		if err == nil && response[2]&2 != 0 {
			response, err = dnsExchange(attempt, dialer, ip, "tcp", packet, h, q)
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			return response, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrDNSUpstream
}

func dnsExchange(ctx context.Context, dialer dnsDialer, ip netip.Addr, network string, packet []byte, h dnsmessage.Header, q dnsmessage.Question) ([]byte, error) {
	conn, err := dialer.DialContext(ctx, network, netip.AddrPortFrom(ip, 53).String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	request := packet
	var size [2]byte
	if network == "tcp" {
		binary.BigEndian.PutUint16(size[:], uint16(len(packet)))
		request = append(size[:], packet...)
	}
	n, err := conn.Write(request)
	if err != nil || n != len(request) {
		return nil, ErrDNSUpstream
	}
	var reply []byte
	if network == "tcp" {
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		reply = make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(conn, reply); err != nil {
			return nil, err
		}
	} else {
		reply = make([]byte, 65535)
		n, err := conn.Read(reply)
		if err != nil {
			return nil, err
		}
		reply = reply[:n]
	}
	rh, rq, err := dnsQuestion(reply)
	if err != nil || !rh.Response || rh.ID != h.ID || rq.Type != q.Type || rq.Class != q.Class || foldDNSName(rq.Name.String()) != foldDNSName(q.Name.String()) || (network == "tcp" && rh.Truncated) {
		return nil, ErrDNSUpstream
	}
	if rh.RCode == dnsmessage.RCodeServerFailure || rh.RCode == dnsmessage.RCodeRefused {
		return nil, ErrDNSUpstream
	}
	return reply, nil
}
