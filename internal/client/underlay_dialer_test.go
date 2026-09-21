package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestMarkedUnderlayCoversTransportAndResolverWithoutFallback(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close() }()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udp.Close() }()
	for _, fail := range []bool{false, true} {
		for _, path := range []string{"transport", "dns_tcp", "dns_udp"} {
			t.Run(path+map[bool]string{false: "_marked", true: "_failure"}[fail], func(t *testing.T) {
				calls := 0
				markError := errors.New("socket mark failed")
				dialer := markedUnderlayDialer(51820, func(raw syscall.RawConn, mark uint32) error {
					calls++
					if raw == nil || mark != 51820 {
						t.Fatal("wrong underlay socket or mark")
					}
					if fail {
						return markError
					}
					return nil
				}, nil, []string{"127.0.0.1"})
				dialer.observeRoute = func(context.Context, string, netip.AddrPort, underlayDNSLink) error { return nil }
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				var conn net.Conn
				var err error
				switch path {
				case "transport":
					conn, err = dialer.DialContext(ctx, "tcp4", tcp.Addr().String())
				case "dns_tcp":
					conn, err = dialer.resolver.socketDial(ctx, "tcp4", netip.MustParseAddrPort(tcp.Addr().String()), underlayDNSLink{})
				case "dns_udp":
					conn, err = dialer.resolver.socketDial(ctx, "udp4", netip.MustParseAddrPort(udp.LocalAddr().String()), underlayDNSLink{})
				}
				if conn != nil {
					defer func() { _ = conn.Close() }()
				}
				if calls != 1 || dialer.direct.Control == nil {
					t.Fatal("underlay bypassed its marked socket path")
				}
				if fail {
					if conn != nil || !errors.Is(err, markError) {
						t.Fatal("mark failure fell back to a connected unmarked socket", err)
					}
				} else if err != nil || conn == nil {
					t.Fatal("marked socket did not connect", err)
				}
			})
		}
	}
}

func TestUnderlayMarkIsExplicitAndUnsupportedPlatformsRejectIt(t *testing.T) {
	dialer := markedUnderlayDialer(0, func(syscall.RawConn, uint32) error {
		t.Fatal("ordinary connection attempted privileged socket marking")
		return nil
	}, nil, nil)
	if dialer.direct.Control != nil || dialer.resolver != nil {
		t.Fatal("ordinary connections changed resolver or socket policy")
	}
	if runtime.GOOS != "linux" {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		conn, err := markedUnderlayDialer(51820, nil, nil, []string{"127.0.0.1"}).DialContext(ctx, "udp4", "127.0.0.1:9")
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil || conn != nil {
			t.Fatal("unsupported platform accepted an unmarked underlay")
		}
	}
}
