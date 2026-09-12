package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"golang.org/x/net/dns/dnsmessage"
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
	Domain    string
	Upstreams []string
}

type DNSProxyOptions struct {
	ListenAddr    string
	UpstreamAddrs []string
	SplitRules    []SplitDNSRule
	NetworkMap    clientapi.RegisterNodeResponse
	SigningTrust  *clientapi.SigningTrustBundle
	ServePeerDNS  bool
	SearchDomain  string
	Timeout       time.Duration
	Ready         func(addr string)
	responseLimit int
}

type dnsQuestion struct {
	Name        string
	Type        uint16
	Class       uint16
	QuestionEnd int
}

func ServeDNSProxy(ctx context.Context, opts DNSProxyOptions) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	listenAddr := strings.TrimSpace(opts.ListenAddr)
	if listenAddr == "" {
		listenAddr = "127.0.0.1:5353"
	}
	tcpListener, udpConn, err := listenDNSProxyPair(listenAddr)
	if err != nil {
		return err
	}
	defer func() { _ = udpConn.Close() }()
	defer func() { _ = tcpListener.Close() }()
	if opts.Ready != nil {
		opts.Ready(udpConn.LocalAddr().String())
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	errC := make(chan error, 2)
	go func() { errC <- serveDNSProxyUDP(serveCtx, udpConn, opts, timeout) }()
	go func() { errC <- serveDNSProxyTCP(serveCtx, tcpListener, opts, timeout) }()
	go func() {
		<-serveCtx.Done()
		_ = udpConn.Close()
		_ = tcpListener.Close()
	}()
	err = <-errC
	parentCanceled := ctx.Err() != nil
	cancel()
	if parentCanceled || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// TCP and UDP have independent port availability. In particular, Windows can
// assign either transport an ephemeral port excluded from the other. Alternate
// the transport choosing the port to avoid walking a range excluded from its
// partner. Explicit ports never move.
func listenDNSProxyPair(address string) (net.Listener, net.PacketConn, error) {
	return listenDNSProxyPairWith(address,
		func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		func(addr string) (net.PacketConn, error) { return net.ListenPacket("udp", addr) })
}

func listenDNSProxyPairWith(address string, listenTCP func(string) (net.Listener, error), listenUDP func(string) (net.PacketConn, error)) (net.Listener, net.PacketConn, error) {
	attempts := 1
	if _, port, err := net.SplitHostPort(address); err == nil && port == "0" {
		attempts = 16
	}
	var lastErr error
	for attempt := range attempts {
		if attempt%2 == 1 {
			udp, err := listenUDP(address)
			if err != nil {
				return nil, nil, err
			}
			tcp, err := listenTCP(udp.LocalAddr().String())
			if err == nil {
				return tcp, udp, nil
			}
			_ = udp.Close()
			lastErr = err
			continue
		}
		tcp, err := listenTCP(address)
		if err != nil {
			return nil, nil, err
		}
		udp, err := listenUDP(tcp.Addr().String())
		if err == nil {
			return tcp, udp, nil
		}
		_ = tcp.Close()
		lastErr = err
	}
	return nil, nil, lastErr
}

func serveDNSProxyUDP(ctx context.Context, conn net.PacketConn, opts DNSProxyOptions, timeout time.Duration) error {
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

func serveDNSProxyTCP(ctx context.Context, listener net.Listener, opts DNSProxyOptions, timeout time.Duration) error {
	opts.responseLimit = 65535
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func() {
			defer func() { _ = conn.Close() }()
			_ = conn.SetDeadline(time.Now().Add(timeout))
			var size [2]byte
			if _, err := io.ReadFull(conn, size[:]); err != nil {
				return
			}
			request := make([]byte, int(binary.BigEndian.Uint16(size[:])))
			if len(request) == 0 {
				return
			}
			if _, err := io.ReadFull(conn, request); err != nil {
				return
			}
			response, err := DNSProxyResponse(ctx, request, opts, timeout)
			if err != nil {
				response = dnsErrorResponse(request, dnsRCodeFail)
			}
			if len(response) == 0 || len(response) > 65535 {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(response)))
			_, _ = conn.Write(append(size[:], response...))
		}()
	}
}

func DNSProxyResponse(ctx context.Context, request []byte, opts DNSProxyOptions, timeout time.Duration) ([]byte, error) {
	question, err := parseDNSQuestion(request)
	if err != nil {
		return dnsErrorResponse(request, dnsRCodeForm), nil
	}
	if response, matched := serviceDNSResponse(request, question, opts); matched {
		return response, nil
	}
	if response, matched := applicationDNSResponse(request, question, opts); matched {
		return response, nil
	}
	if opts.ServePeerDNS {
		searchDomain := normalizeDNSName(opts.SearchDomain)
		if searchDomain == "" {
			searchDomain = DefaultDNSDomain(opts.NetworkMap.Network.Name)
		}
		if dnsNameInDomain(question.Name, searchDomain) {
			return dnsPeerResponse(request, question, opts.NetworkMap, searchDomain), nil
		}
	}
	if upstreams, matched := selectSplitDNSUpstreams(question.Name, opts.SplitRules); matched {
		if len(upstreams) == 0 {
			return dnsErrorResponse(request, dnsRCodeFail), nil
		}
		return forwardDNSQueryAny(ctx, upstreams, request, timeout)
	}
	if len(opts.UpstreamAddrs) == 0 {
		return dnsErrorResponse(request, dnsRCodeFail), nil
	}
	return forwardDNSQueryAny(ctx, opts.UpstreamAddrs, request, timeout)
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
		if peerDNSNameExists(networkMap, question.Name, searchDomain) {
			// The owner name exists but has no address in the requested family.
			// NXDOMAIN would poison the other family in validating/system caches.
			return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil)
		}
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

func peerDNSNameExists(networkMap clientapi.RegisterNodeResponse, query, searchDomain string) bool {
	label, _, err := resolveDNSQueryLabel(query, normalizeDNSName(searchDomain))
	if err != nil {
		return false
	}
	for _, record := range peerDNSRecords(networkMap) {
		if record.label == label {
			return true
		}
	}
	return false
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
	response := append([]byte(nil), buf[:n]...)
	if err := matchDNSUpstreamResponse(request, response); err != nil {
		return nil, err
	}
	if len(response) >= 4 && binary.BigEndian.Uint16(response[2:4])&0x0200 != 0 {
		return forwardDNSQueryTCP(ctx, upstream, request, timeout)
	}
	return response, nil
}

func forwardDNSQueryAny(ctx context.Context, upstreams []string, request []byte, timeout time.Duration) ([]byte, error) {
	var lastErr error
	for _, upstream := range upstreams {
		upstream = strings.TrimSpace(upstream)
		if upstream == "" {
			continue
		}
		response, err := forwardDNSQuery(ctx, upstream, request, timeout)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no DNS upstream is available")
	}
	return nil, lastErr
}

func forwardDNSQueryTCP(ctx context.Context, upstream string, request []byte, timeout time.Duration) ([]byte, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", strings.TrimSpace(upstream))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if len(request) > 65535 {
		return nil, errors.New("DNS query exceeds TCP framing limit")
	}
	framed := make([]byte, 2, len(request)+2)
	binary.BigEndian.PutUint16(framed, uint16(len(request)))
	framed = append(framed, request...)
	if _, err := conn.Write(framed); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, framed[:2]); err != nil {
		return nil, err
	}
	response := make([]byte, int(binary.BigEndian.Uint16(framed[:2])))
	if _, err := io.ReadFull(conn, response); err != nil {
		return nil, err
	}
	if err := matchDNSUpstreamResponse(request, response); err != nil {
		return nil, err
	}
	return response, nil
}

// Match the upstream message to the query before trusting its answer or TC bit.
// Connected sockets already constrain the remote endpoint. Parse the question
// section independently: a legitimate truncated reply need not contain complete
// answer records, but must still belong to this transaction.
func matchDNSUpstreamResponse(request, response []byte) error {
	var query, reply dnsmessage.Parser
	qh, err := query.Start(request)
	if err != nil {
		return errors.New("invalid DNS upstream query")
	}
	questions, err := query.AllQuestions()
	if err != nil || len(questions) != 1 {
		return errors.New("invalid DNS upstream query question")
	}
	rh, err := reply.Start(response)
	if err != nil || !rh.Response || rh.ID != qh.ID || rh.OpCode != qh.OpCode {
		return errors.New("DNS upstream response header mismatch")
	}
	answers, err := reply.AllQuestions()
	if err != nil || len(answers) != 1 || answers[0].Type != questions[0].Type || answers[0].Class != questions[0].Class || !strings.EqualFold(answers[0].Name.String(), questions[0].Name.String()) {
		return errors.New("DNS upstream response question mismatch")
	}
	return nil
}

// The most specific suffix owns the query, including an unavailable (empty)
// upstreams. Never fall through to a parent or global resolver for that name.
// Equal-specificity declarations are combined in priority order.
func selectSplitDNSUpstreams(name string, rules []SplitDNSRule) ([]string, bool) {
	var selected []string
	specificity := 0
	for _, rule := range rules {
		domain := normalizeDNSName(rule.Domain)
		if !dnsNameInDomain(name, domain) || len(domain) < specificity {
			continue
		}
		if len(domain) > specificity {
			selected = nil
			specificity = len(domain)
		}
		for _, upstream := range rule.Upstreams {
			if upstream = strings.TrimSpace(upstream); upstream != "" && !slices.Contains(selected, upstream) {
				selected = append(selected, upstream)
			}
		}
	}
	return selected, specificity > 0
}

func dnsNameInDomain(name, domain string) bool {
	name = normalizeDNSName(name)
	domain = normalizeDNSName(domain)
	return domain != "" && (name == domain || strings.HasSuffix(name, "."+domain))
}
