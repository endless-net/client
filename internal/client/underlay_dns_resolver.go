package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

type underlayDNSSocketDial func(context.Context, string, netip.AddrPort, underlayDNSLink) (net.Conn, error)
type underlayDNSResolver struct {
	source     *underlayDNSSource
	socketDial underlayDNSSocketDial
	invalid    error
}

func newUnderlayDNSResolver(source *underlayDNSSource, dial underlayDNSSocketDial) *underlayDNSResolver {
	r := &underlayDNSResolver{socketDial: dial}
	if source == nil || source.Owner == "" || len(source.Links) == 0 || len(source.Links) > 64 {
		r.invalid = errors.New("underlay DNS source unavailable")
		return r
	}
	copy := cloneUnderlayDNSSource(source)
	indices := map[int]bool{}
	for i := range copy.Links {
		link := &copy.Links[i]
		link.Servers = slices.Clone(link.Servers)
		link.Domains = slices.Clone(link.Domains)
		if link.Index < 0 || indices[link.Index] || link.DNSSEC != "no" || link.DNSOverTLS != "no" || len(link.Servers) > 16 || len(link.Domains) > 64 || (link.Index == 0 && link.Name != "") || (link.Index != 0 && (!safeWireGuardInterfaceName(link.Name) || link.Name == "lo" || strings.TrimSpace(link.Name) != link.Name)) {
			r.invalid = errors.New("unsupported underlay DNS source")
			return r
		}
		indices[link.Index] = true
		for _, server := range link.Servers {
			ip := server.Addr()
			if !server.IsValid() || server.Port() == 0 || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || (ip.Zone() != "" && ip.Zone() != link.Name) || (ip.Is6() && ip.IsLinkLocalUnicast() && ip.Zone() == "") {
				r.invalid = errors.New("invalid direct underlay DNS server")
				return r
			}
		}
		for _, domain := range link.Domains {
			name := domain.Name
			if name == "." {
				continue
			}
			canonical, err := underlayHost("x." + name)
			if err != nil || canonical != "x."+name {
				r.invalid = errors.New("invalid underlay DNS routing domain")
				return r
			}
		}
	}
	r.source = copy
	return r
}

func (r *underlayDNSResolver) links(host string) ([]underlayDNSLink, error) {
	if r == nil || r.invalid != nil || r.source == nil {
		return nil, errors.New("underlay DNS source unavailable")
	}
	best := -1
	var selected []underlayDNSLink
	for _, link := range r.source.Links {
		specificity := -1
		for _, domain := range link.Domains {
			if domain.Name == "." {
				specificity = max(specificity, 0)
			} else if host == domain.Name || strings.HasSuffix(host, "."+domain.Name) {
				specificity = max(specificity, strings.Count(domain.Name, ".")+1)
			}
		}
		if specificity < 0 || specificity < best {
			continue
		}
		if specificity > best {
			selected = nil
			best = specificity
		}
		selected = append(selected, link)
	}
	if best < 0 {
		for _, link := range r.source.Links {
			if link.DefaultRoute || link.Index == 0 {
				selected = append(selected, link)
			}
		}
	}
	if strings.HasSuffix(host, ".local") && best <= 0 {
		return nil, errors.New("underlay multicast name has no explicit DNS route")
	}
	if len(selected) == 0 {
		return nil, errors.New("underlay DNS has no matching route")
	}
	return selected, nil
}

func (r *underlayDNSResolver) lookup(ctx context.Context, host, network string) ([]netip.Addr, error) {
	types := []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA}
	if strings.HasSuffix(network, "4") {
		types = types[:1]
	} else if strings.HasSuffix(network, "6") {
		types = types[1:]
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	results := make(chan []netip.Addr, len(types))
	for _, kind := range types {
		workers.Add(1)
		go func() { defer workers.Done(); results <- r.lookupFamily(work, host, kind) }()
	}
	var addresses []netip.Addr
	// Complete both families within the shared DNS budget. A slower usable
	// family must remain available if connections to the faster family fail.
	for range types {
		select {
		case answer := <-results:
			for _, ip := range answer {
				if !slices.Contains(addresses, ip) {
					addresses = append(addresses, ip)
				}
			}
		case <-work.Done():
		}
		if work.Err() != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("underlay DNS did not confirm an address")
	}
	return addresses, nil
}

type underlayDNSAnswer struct {
	addresses []netip.Addr
	aliases   []string
}

func (r *underlayDNSResolver) lookupFamily(ctx context.Context, host string, kind dnsmessage.Type) []netip.Addr {
	seen := map[string]bool{host: true}
	for {
		if ctx.Err() != nil {
			return nil
		}
		links, err := r.links(host)
		if err != nil {
			return nil
		}
		var answer underlayDNSAnswer
		found := false
		for _, link := range links {
			for _, server := range link.Servers {
				if ctx.Err() != nil {
					return nil
				}
				candidate, err := r.query(ctx, host, kind, link, server)
				if err == nil && (len(candidate.addresses) > 0 || len(candidate.aliases) > 0) {
					answer = candidate
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return nil
		}
		for _, alias := range answer.aliases {
			if seen[alias] || len(seen) >= 9 {
				return nil
			}
			seen[alias] = true
			host = alias
		}
		if len(answer.addresses) > 0 {
			return answer.addresses
		}
		// Only a CNAME bound to the preceding validated DNS response/question can
		// extend the lookup. Re-select its source route; never try a parent route.
	}
}

func (r *underlayDNSResolver) query(ctx context.Context, host string, kind dnsmessage.Type, link underlayDNSLink, server netip.AddrPort) (underlayDNSAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var id [2]byte
	if _, err := rand.Read(id[:]); err != nil {
		return underlayDNSAnswer{}, err
	}
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return underlayDNSAnswer{}, err
	}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: kind, Class: dnsmessage.ClassINET}}}
	request, err := query.Pack()
	if err != nil {
		return underlayDNSAnswer{}, err
	}
	response, err := r.exchange(ctx, "udp", server, link, request)
	if err != nil {
		return underlayDNSAnswer{}, err
	}
	if err := matchDNSUpstreamResponse(request, response); err != nil {
		return underlayDNSAnswer{}, err
	}
	if binary.BigEndian.Uint16(response[2:4])&0x200 != 0 {
		response, err = r.exchange(ctx, "tcp", server, link, request)
		if err != nil {
			return underlayDNSAnswer{}, err
		}
		if err := matchDNSUpstreamResponse(request, response); err != nil {
			return underlayDNSAnswer{}, err
		}
	}
	return underlayDNSAddresses(response, host, kind)
}

func (r *underlayDNSResolver) exchange(ctx context.Context, network string, server netip.AddrPort, link underlayDNSLink, request []byte) ([]byte, error) {
	conn, err := r.socketDial(ctx, network, server, link)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("DNS exchange requires deadline")
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	packet := request
	if network == "tcp" {
		packet = make([]byte, len(request)+2)
		binary.BigEndian.PutUint16(packet, uint16(len(request)))
		copy(packet[2:], request)
	}
	if n, err := conn.Write(packet); err != nil {
		return nil, err
	} else if n != len(packet) {
		return nil, io.ErrShortWrite
	}
	if network == "tcp" {
		var header [2]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(header[:]))
		if length < 12 {
			return nil, errors.New("invalid DNS response length")
		}
		response := make([]byte, length)
		_, err := io.ReadFull(conn, response)
		return response, err
	}
	response := make([]byte, 65535)
	n, err := conn.Read(response)
	if err != nil {
		return nil, err
	}
	return response[:n], nil
}

func underlayDNSAddresses(raw []byte, host string, kind dnsmessage.Type) (underlayDNSAnswer, error) {
	var result underlayDNSAnswer
	var reply dnsmessage.Message
	if len(raw) > 65535 || reply.Unpack(raw) != nil || reply.Truncated || reply.RCode != dnsmessage.RCodeSuccess || len(reply.Answers) > 128 {
		return result, errors.New("invalid underlay DNS answer")
	}
	aliases := map[string]string{}
	addresses := map[string][]netip.Addr{}
	for _, answer := range reply.Answers {
		if answer.Header.Class != dnsmessage.ClassINET {
			continue
		}
		owner := strings.ToLower(strings.TrimSuffix(answer.Header.Name.String(), "."))
		switch body := answer.Body.(type) {
		case *dnsmessage.CNAMEResource:
			alias := strings.ToLower(strings.TrimSuffix(body.CNAME.String(), "."))
			canonical, err := underlayHost(alias)
			if err != nil || canonical != alias {
				return result, errors.New("invalid DNS alias")
			}
			if prior, ok := aliases[owner]; ok && prior != alias {
				return result, errors.New("ambiguous DNS alias")
			}
			aliases[owner] = alias
		case *dnsmessage.AResource:
			if kind == dnsmessage.TypeA {
				addresses[owner] = append(addresses[owner], netip.AddrFrom4(body.A))
			}
		case *dnsmessage.AAAAResource:
			if kind == dnsmessage.TypeAAAA {
				addresses[owner] = append(addresses[owner], netip.AddrFrom16(body.AAAA))
			}
		}
	}
	seen := map[string]bool{}
	for range 9 {
		if seen[host] {
			return result, errors.New("cyclic DNS alias")
		}
		seen[host] = true
		alias, exists := aliases[host]
		if !exists {
			for _, ip := range addresses[host] {
				if ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.Is4In6() {
					result.addresses = append(result.addresses, ip)
				}
			}
			return result, nil
		}
		if len(addresses[host]) != 0 {
			return result, errors.New("ambiguous DNS alias address")
		}
		result.aliases = append(result.aliases, alias)
		host = alias
	}
	return result, errors.New("DNS alias depth exceeded")
}
