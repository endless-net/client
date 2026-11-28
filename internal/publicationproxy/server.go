package publicationproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	"golang.org/x/crypto/acme"
)

type serverRoutes interface {
	RouteSource
	AllowsCertificate(string) bool
}

// A separate value for each accepted connection preserves the actual leaf expiry
// for keepalive and HTTP/2 requests across a certificate rotation.
type connectionKey struct{}
type servedCertificate struct {
	mu    sync.Mutex
	until time.Time
	peer  *api.PublicationPeer
}

// NewServer serves certificates from the supplied issuer (including ACME
// TLS-ALPN-01). authenticatedPeer may be provided by a trusted overlay listener.
// It must attest the connection, independently of HTTP headers or source claims.
func NewServer(routes serverRoutes, issuer func(*tls.ClientHelloInfo) (*tls.Certificate, error), dial func(context.Context, netip.AddrPort) (net.Conn, error), authenticatedPeer func(net.Conn) *api.PublicationPeer, now func() time.Time) (*http.Server, error) {
	if routes == nil || issuer == nil || dial == nil || now == nil {
		return nil, connectivity.ErrConfig
	}
	h := &Handler{Routes: routes, DialOverlay: dial, Now: now, Identity: func(r *http.Request) ConnectionIdentity {
		state, ok := r.Context().Value(connectionKey{}).(*servedCertificate)
		if !ok {
			return ConnectionIdentity{}
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		return ConnectionIdentity{CertificateUntil: state.until, Peer: state.peer}
	}}
	config := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1", acme.ALPNProto}, SessionTicketsDisabled: true}
	config.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return selectCertificate(hello.Context(), hello, routes, issuer, now)
	}
	return &http.Server{TLSConfig: config, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 64 << 10,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			state := new(servedCertificate)
			if authenticatedPeer != nil {
				state.peer = authenticatedPeer(c)
			}
			return context.WithValue(ctx, connectionKey{}, state)
		}}, nil
}

func selectCertificate(ctx context.Context, hello *tls.ClientHelloInfo, routes serverRoutes, issuer func(*tls.ClientHelloInfo) (*tls.Certificate, error), now func() time.Time) (*tls.Certificate, error) {
	state, ok := ctx.Value(connectionKey{}).(*servedCertificate)
	host := strings.ToLower(hello.ServerName)
	if !ok || !routes.AllowsCertificate(host) {
		return nil, connectivity.ErrPublication
	}
	cert, err := issuer(hello)
	if err != nil {
		return nil, err
	}
	if cert == nil || len(cert.Certificate) == 0 {
		return nil, connectivity.ErrPublication
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	instant := now()
	if leaf.VerifyHostname(host) != nil || instant.Before(leaf.NotBefore) || !instant.Before(leaf.NotAfter) || !routes.AllowsCertificate(host) {
		return nil, connectivity.ErrPublication
	}
	state.mu.Lock()
	state.until = leaf.NotAfter
	state.mu.Unlock()
	return cert, nil
}
