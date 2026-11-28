package publicationproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

func TestPublicationProbeBindsOverlayPortAndRechecksGrant(t *testing.T) {
	now := time.Now()
	target := connectivity.PublicationTarget{Address: netip.MustParseAddrPort("100.70.0.2:8443"), TLSName: "docs.internal", Until: now.Add(time.Minute), Generation: t.Context()}
	resolve := func(host string) (connectivity.PublicationTarget, error) {
		if host != "docs.example" {
			return connectivity.PublicationTarget{}, connectivity.ErrPublication
		}
		return target, nil
	}
	var backend net.Conn
	dial := func(_ context.Context, to netip.AddrPort) (net.Conn, error) {
		if to != target.Address {
			t.Fatal(to)
		}
		a, b := net.Pipe()
		backend = b
		return a, nil
	}
	check := func(host string) bool { return probeBackend(t.Context(), host, resolve, dial) }
	if check("unknown.example") || backend != nil {
		t.Fatal("unapproved destination reached")
	}
	if !check("docs.example") {
		t.Fatal("live TCP target rejected")
	}
	var buf [1]byte
	if n, err := backend.Read(buf[:]); n != 0 || err != io.EOF {
		t.Fatal("application bytes written", n, err)
	}
	_ = backend.Close()
	target.HTTPS = true
	hello := make(chan string, 1)
	ok := probeBackend(t.Context(), "docs.example", resolve, func(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
		a, err := dial(ctx, to)
		b := backend
		go func() {
			defer func() { _ = b.Close() }()
			server := tls.Server(b, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
				hello <- h.ServerName
				return nil, errors.New("no certificate")
			}})
			_ = server.HandshakeContext(ctx)
		}()
		return a, err
	})
	if ok {
		t.Fatal("TCP accept sufficed for HTTPS")
	}
	select {
	case h := <-hello:
		if h != "docs.internal" {
			t.Fatal(h)
		}
	default:
		t.Fatal("no backend TLS hello")
	}
	target.HTTPS = false
	ok = probeBackend(t.Context(), "docs.example", resolve, func(ctx context.Context, to netip.AddrPort) (net.Conn, error) {
		a, err := dial(ctx, to)
		target.Address = netip.MustParseAddrPort("100.70.0.3:8443")
		return a, err
	})
	_ = backend.Close()
	if ok {
		t.Fatal("changed target counted as healthy")
	}
}
