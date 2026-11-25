package clientapp

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

func (s *Signals) newControlHTTPClient(timeout time.Duration, maxConnections int) (*http.Client, error) {
	if timeout <= 0 || maxConnections <= 0 {
		return nil, ErrSignals
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: timeout, IdleConnTimeout: timeout,
		MaxIdleConns: maxConnections, MaxIdleConnsPerHost: maxConnections, MaxConnsPerHost: maxConnections,
	}
	observed, err := s.HTTPTransport(transport)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: observed, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrSignals }}, nil
}
