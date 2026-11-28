// Package publicationproxy serves HTTPS publications over a registered node's overlay.
package publicationproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
)

type RouteSource interface {
	Resolve(string, *api.PublicationPeer) (connectivity.PublicationTarget, error)
}

// ConnectionIdentity is supplied by the listener using the certificate served
// on this connection and, on an authenticated overlay listener, the remote peer.
type ConnectionIdentity struct {
	CertificateUntil time.Time
	Peer             *api.PublicationPeer
}
type Handler struct {
	Routes      RouteSource
	Identity    func(*http.Request) ConnectionIdentity
	DialOverlay func(context.Context, netip.AddrPort) (net.Conn, error)
	Now         func() time.Time
	Logger      *slog.Logger
}

func logPublicationRequest(ctx context.Context, logger *slog.Logger, outcome, publication string, backendStatus int) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "publication.request", "outcome", outcome, "hostname", publication, "backend_status", backendStatus)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	outcome := "access_denied"
	var publication string
	var backendStatus int
	defer func() {
		logPublicationRequest(r.Context(), h.Logger, outcome, publication, backendStatus)
	}()
	if h.Routes == nil || h.Identity == nil || h.DialOverlay == nil || h.Now == nil || r.TLS == nil || r.TLS.ServerName == "" || r.Method == http.MethodConnect {
		http.NotFound(w, r)
		return
	}
	host := strings.ToLower(r.Host)
	if name, port, err := net.SplitHostPort(host); err == nil {
		if port != "443" {
			http.NotFound(w, r)
			return
		}
		host = name
	}
	if host != strings.ToLower(r.TLS.ServerName) {
		http.NotFound(w, r)
		return
	}
	identity := h.Identity(r)
	if !h.Now().Before(identity.CertificateUntil) {
		http.NotFound(w, r)
		return
	}
	target, err := h.Routes.Resolve(host, identity.Peer)
	if err != nil {
		if errors.Is(err, connectivity.ErrPublicationTarget) {
			publication, outcome = host, "target_unavailable"
		}
		http.NotFound(w, r)
		return
	}
	publication, outcome = host, "target_unavailable"
	end := target.Until
	if identity.CertificateUntil.Before(end) {
		end = identity.CertificateUntil
	}
	ctx, cancel := context.WithTimeout(r.Context(), end.Sub(h.Now()))
	defer cancel()
	stop := context.AfterFunc(target.Generation, cancel)
	defer stop()
	current := func() bool {
		if ctx.Err() != nil || target.Generation.Err() != nil || !h.Now().Before(end) {
			return false
		}
		next, err := h.Routes.Resolve(host, identity.Peer)
		return err == nil && next.Generation == target.Generation && next.Peer == target.Peer && next.Address == target.Address && next.TLSName == target.TLSName && next.HTTPS == target.HTTPS
	}
	transport := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.TLSName},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if !current() {
				return nil, connectivity.ErrPublication
			}
			conn, err := h.DialOverlay(ctx, target.Address)
			if err != nil {
				return nil, err
			}
			return &publicationConn{Conn: conn, current: current}, nil
		},
	}
	defer transport.CloseIdleConnections()
	dest := &url.URL{Scheme: "http", Host: target.TLSName}
	if target.HTTPS {
		dest.Scheme = "https"
	}
	proxy := httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(dest)
		// Browser-controlled headers do not attest an overlay identity.
		for _, name := range []string{"X-Node-ID", "X-Account-ID", "X-User-ID", "X-Role", "X-Forwarded-Client-Cert"} {
			p.Out.Header.Del(name)
		}
	}, ModifyResponse: func(response *http.Response) error {
		outcome, backendStatus = "forwarded", response.StatusCode
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		if errors.Is(err, connectivity.ErrPublication) || target.Generation.Err() != nil {
			outcome = "access_denied"
		}
		http.Error(w, "service unavailable", http.StatusBadGateway)
	}}
	proxy.ServeHTTP(w, r.WithContext(ctx))
}

type publicationConn struct {
	net.Conn
	current func() bool
}

func (c *publicationConn) Write(p []byte) (int, error) {
	if !c.current() {
		_ = c.Close()
		return 0, connectivity.ErrPublication
	}
	return c.Conn.Write(p)
}
func (c *publicationConn) Read(p []byte) (int, error) {
	if !c.current() {
		_ = c.Close()
		return 0, connectivity.ErrPublication
	}
	n, err := c.Conn.Read(p)
	if n > 0 && !c.current() {
		clear(p[:n])
		_ = c.Close()
		return 0, connectivity.ErrPublication
	}
	return n, err
}
