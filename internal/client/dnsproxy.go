package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

const (
	dnsTypeA       uint16 = 1
	dnsTypeAAAA    uint16 = 28
	dnsClassIN     uint16 = 1
	dnsRCodeNoErr  uint16 = 0
	dnsRCodeForm   uint16 = 1
	dnsRCodeNX     uint16 = 3
	dnsRCodeFail   uint16 = 2
	dnsMaxUDPBytes        = 1232
)

type SplitDNSRule struct {
	Domain   string
	Upstream string
}

type DNSProxyOptions struct {
	ListenAddr   string
	UpstreamAddr string
	SplitRules   []SplitDNSRule
	NetworkMap   clientapi.RegisterNodeResponse
	SearchDomain string
	Timeout      time.Duration
	Ready        func(addr string)
}

type dnsQuestion struct {
	Name        string
	Type        uint16
	Class       uint16
	QuestionEnd int
}

func ServeDNSProxy(ctx context.Context, opts DNSProxyOptions) error {
	listenAddr := strings.TrimSpace(opts.ListenAddr)
	if listenAddr == "" {
		listenAddr = "127.0.0.1:5353"
	}
	conn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if opts.Ready != nil {
		opts.Ready(conn.LocalAddr().String())
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	buf := make([]byte, dnsMaxUDPBytes)
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		request := append([]byte(nil), buf[:n]...)
		go func() {
			response, err := DNSProxyResponse(ctx, request, opts, timeout)
			if err != nil {
				response = dnsErrorResponse(request, dnsRCodeFail)
			}
			if len(response) > 0 {
				_, _ = conn.WriteTo(response, addr)
			}
		}()
	}
}

func DNSProxyResponse(ctx context.Context, request []byte, opts DNSProxyOptions, timeout time.Duration) ([]byte, error) {
	question, err := parseDNSQuestion(request)
	if err != nil {
		return dnsErrorResponse(request, dnsRCodeForm), nil
	}
	searchDomain := normalizeDNSName(opts.SearchDomain)
	if searchDomain == "" {
		searchDomain = DefaultDNSDomain(opts.NetworkMap.Network.Name)
	}
	if dnsNameInDomain(question.Name, searchDomain) {
		return dnsPeerResponse(request, question, opts.NetworkMap, searchDomain), nil
	}
	if upstream := splitDNSUpstream(question.Name, opts.SplitRules); upstream != "" {
		return forwardDNSQuery(ctx, upstream, request, timeout)
	}
	if splitDNSDomainMatches(question.Name, opts.SplitRules) {
		return dnsErrorResponse(request, dnsRCodeFail), nil
	}
	upstream := strings.TrimSpace(opts.UpstreamAddr)
	if upstream == "" {
		return dnsErrorResponse(request, dnsRCodeFail), nil
	}
	return forwardDNSQuery(ctx, upstream, request, timeout)
}

func dnsPeerResponse(request []byte, question dnsQuestion, networkMap clientapi.RegisterNodeResponse, searchDomain string) []byte {
	var family DNSAddressFamily
	switch question.Type {
	case dnsTypeA:
		family = DNSAddressIPv4
	case dnsTypeAAAA:
		family = DNSAddressIPv6
	default:
		return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil)
	}
	if question.Class != dnsClassIN {
		return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil)
	}
	resolution, err := ResolvePeerDNSName(networkMap, question.Name, searchDomain, family)
	if err != nil {
		return dnsErrorResponse(request, dnsRCodeNX)
	}
	addr, err := netip.ParseAddr(resolution.Address)
	if err != nil {
		return dnsErrorResponse(request, dnsRCodeFail)
	}
	var rdata []byte
	if question.Type == dnsTypeA {
		raw := addr.As4()
		rdata = raw[:]
	} else {
		raw := addr.As16()
		rdata = raw[:]
	}
	answer := make([]byte, 0, 16+len(rdata))
	answer = append(answer, 0xc0, 0x0c)
	answer = binary.BigEndian.AppendUint16(answer, question.Type)
	answer = binary.BigEndian.AppendUint16(answer, dnsClassIN)
	answer = binary.BigEndian.AppendUint32(answer, uint32(DefaultDNSTTLSeconds))
	answer = binary.BigEndian.AppendUint16(answer, uint16(len(rdata)))
	answer = append(answer, rdata...)
	return dnsResponseHeader(request, dnsRCodeNoErr, 1, answer)
}

func parseDNSQuestion(packet []byte) (dnsQuestion, error) {
	if len(packet) < 12 {
		return dnsQuestion{}, io.ErrUnexpectedEOF
	}
	if binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return dnsQuestion{}, fmt.Errorf("expected exactly one DNS question")
	}
	offset := 12
	labels := []string{}
	for {
		if offset >= len(packet) {
			return dnsQuestion{}, io.ErrUnexpectedEOF
		}
		l := int(packet[offset])
		offset++
		if l == 0 {
			break
		}
		if l&0xc0 != 0 || l > 63 || offset+l > len(packet) {
			return dnsQuestion{}, fmt.Errorf("invalid DNS query name")
		}
		labels = append(labels, string(packet[offset:offset+l]))
		offset += l
	}
	if offset+4 > len(packet) {
		return dnsQuestion{}, io.ErrUnexpectedEOF
	}
	return dnsQuestion{
		Name:        normalizeDNSName(strings.Join(labels, ".")),
		Type:        binary.BigEndian.Uint16(packet[offset : offset+2]),
		Class:       binary.BigEndian.Uint16(packet[offset+2 : offset+4]),
		QuestionEnd: offset + 4,
	}, nil
}

func dnsResponseHeader(request []byte, rcode uint16, answerCount uint16, answers []byte) []byte {
	if len(request) < 12 {
		return nil
	}
	response := make([]byte, 12, 12+len(request[12:])+len(answers))
	copy(response[:2], request[:2])
	flags := uint16(0x8000) | uint16(0x0100) | uint16(0x0080) | (rcode & 0x000f)
	binary.BigEndian.PutUint16(response[2:4], flags)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], answerCount)
	binary.BigEndian.PutUint16(response[8:10], 0)
	binary.BigEndian.PutUint16(response[10:12], 0)
	questionEnd := len(request)
	if question, err := parseDNSQuestion(request); err == nil {
		questionEnd = question.QuestionEnd
	}
	response = append(response, request[12:questionEnd]...)
	response = append(response, answers...)
	return response
}

func dnsErrorResponse(request []byte, rcode uint16) []byte {
	return dnsResponseHeader(request, rcode, 0, nil)
}

func forwardDNSQuery(ctx context.Context, upstream string, request []byte, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "udp", strings.TrimSpace(upstream))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(timeout)
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(request); err != nil {
		return nil, err
	}
	buf := make([]byte, dnsMaxUDPBytes)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

func splitDNSUpstream(name string, rules []SplitDNSRule) string {
	for _, rule := range rules {
		domain := normalizeDNSName(rule.Domain)
		upstream := strings.TrimSpace(rule.Upstream)
		if upstream != "" && dnsNameInDomain(name, domain) {
			return upstream
		}
	}
	return ""
}

func splitDNSDomainMatches(name string, rules []SplitDNSRule) bool {
	for _, rule := range rules {
		if strings.TrimSpace(rule.Upstream) == "" && dnsNameInDomain(name, normalizeDNSName(rule.Domain)) {
			return true
		}
	}
	return false
}

func dnsNameInDomain(name, domain string) bool {
	name = normalizeDNSName(name)
	domain = normalizeDNSName(domain)
	return domain != "" && (name == domain || strings.HasSuffix(name, "."+domain))
}
