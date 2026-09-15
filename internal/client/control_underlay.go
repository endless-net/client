package client

import (
	"errors"
	"net/http"
	"net/url"
	"syscall"
)

type controlUnderlayTransport struct {
	base    *http.Transport
	origins map[string]struct{}
}

func newControlUnderlayHTTPClient(origins []string, mark uint32, setMark func(syscall.RawConn, uint32) error) (*http.Client, error) {
	allowed := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		origin, err := rpcProfileOrigin(raw)
		if err != nil {
			return nil, errors.New("control underlay requires HTTPS origins")
		}
		allowed[origin] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, errors.New("control underlay has no authorized origin")
	}
	client := applicationHTTPClient()
	transport := client.Transport.(*http.Transport)
	if mark != 0 {
		// A proxy is a separate underlay authority, not an implicit exemption
		// derived from HTTP_PROXY/HTTPS_PROXY on the native service host.
		transport.Proxy = nil
		// TLS-specific hooks bypass DialContext and therefore its socket mark.
		transport.DialTLSContext = nil
		transport.DialTLS = nil //nolint:staticcheck // Disable the inherited deprecated hook rather than allowing it to bypass marking.
		transport.DialContext = markedUnderlayDialer(mark, setMark).DialContext
	}
	client.Transport = &controlUnderlayTransport{base: transport, origins: allowed}
	return client, nil
}

func (t *controlUnderlayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	allowed := false
	if request != nil && request.URL != nil && request.URL.Fragment == "" {
		u := *request.URL
		u.Path, u.RawPath, u.RawQuery, u.Fragment, u.RawFragment = "", "", "", "", ""
		u.ForceQuery = false
		origin, err := rpcProfileOrigin(u.String())
		_, allowed = t.origins[origin]
		allowed = allowed && err == nil
		if request.Host != "" {
			hostOrigin, err := rpcProfileOrigin((&url.URL{Scheme: u.Scheme, Host: request.Host}).String())
			allowed = allowed && err == nil && hostOrigin == origin
		}
	}
	if !allowed {
		if request != nil && request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, errors.New("control underlay request is outside its authorized origins")
	}
	return t.base.RoundTrip(request)
}

func (t *controlUnderlayTransport) CloseIdleConnections() {
	t.base.CloseIdleConnections()
}
