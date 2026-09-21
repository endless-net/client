package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type underlayDialer struct {
	direct   net.Dialer
	marked   bool
	hosts    map[string]struct{}
	resolver *underlayDNSResolver
	invalid  error
	// Set before publication; revalidate identity without replacing the source.
	current func(context.Context) error
}

func markedUnderlayDialer(mark uint32, setMark func(syscall.RawConn, uint32) error, source *underlayDNSSource, hosts []string) *underlayDialer {
	d := &underlayDialer{marked: mark != 0}
	if mark == 0 {
		return d
	}
	if setMark == nil {
		setMark = setUnderlaySocketMark
	}
	d.direct.Control = func(_, _ string, raw syscall.RawConn) error { return setMark(raw, mark) }
	d.hosts = make(map[string]struct{}, len(hosts))
	if len(hosts) == 0 || len(hosts) > 256 {
		d.invalid = errors.New("underlay requires bounded authorized hosts")
	}
	for _, host := range hosts {
		canonical, err := underlayHost(host)
		if err != nil {
			d.invalid = err
			break
		}
		d.hosts[canonical] = struct{}{}
	}
	d.resolver = newUnderlayDNSResolver(source, func(ctx context.Context, network string, server netip.AddrPort, link underlayDNSLink) (net.Conn, error) {
		dnsDialer := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
			if err := setMark(raw, mark); err != nil {
				return err
			}
			if link.Index != 0 {
				return setUnderlayDNSSocketLink(raw, link.Index)
			}
			return nil
		}}
		return dnsDialer.DialContext(ctx, network, server.String())
	})
	return d
}

func (d *underlayDialer) checkCurrent(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.current != nil {
		return d.current(ctx)
	}
	return nil
}

func (d *underlayDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !d.marked {
		return d.direct.DialContext(ctx, network, address)
	}
	if d.invalid != nil {
		return nil, d.invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := d.checkCurrent(ctx); err != nil {
		return nil, err
	}
	switch network {
	case "tcp", "tcp4", "tcp6", "udp", "udp4", "udp6":
	default:
		return nil, errors.New("unsupported underlay transport")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid underlay endpoint")
	}
	canonical, err := underlayHost(host)
	if err != nil {
		return nil, err
	}
	if _, ok := d.hosts[canonical]; !ok {
		return nil, errors.New("underlay hostname is not authorized")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return nil, errors.New("underlay endpoint requires a numeric port")
	}
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(canonical); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = d.resolver.lookup(ctx, canonical, network)
		if err != nil {
			return nil, err
		}
	}
	if err := d.checkCurrent(ctx); err != nil {
		return nil, err
	}
	return dialUnderlayAddresses(ctx, network, port, addresses, d.direct.DialContext, d.checkCurrent)
}

func underlayHost(host string) (string, error) {
	if host == "" || strings.TrimSpace(host) != host {
		return "", errors.New("invalid underlay hostname")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String(), nil
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if len(name) > 253 {
		return "", errors.New("underlay hostname is too long")
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid underlay hostname")
		}
		for _, char := range label {
			if char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return "", errors.New("invalid underlay hostname")
			}
		}
	}
	return name, nil
}
