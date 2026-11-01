//go:build windows

package clientapp

import (
	"crypto/tls"
	"net"
	"net/http"
)

// NewHTTPClient prepares the native control transport without opening a socket.
// Control adapters keep their own trust/proof/response validation; this client
// adds no workload credential, cookie jar, retry or registration fallback.
func (s *Signals) NewHTTPClient(config Config) (*http.Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: config.CallTimeout}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: config.CallTimeout, ResponseHeaderTimeout: config.CallTimeout,
		ExpectContinueTimeout: config.CallTimeout, IdleConnTimeout: config.CallTimeout,
		MaxIdleConns: config.MaxConnections, MaxIdleConnsPerHost: config.MaxConnections, MaxConnsPerHost: config.MaxConnections,
	}
	observed, err := s.HTTPTransport(transport)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: observed, Timeout: config.CallTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrSignals }}, nil
}
