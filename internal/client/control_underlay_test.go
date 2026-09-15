package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

type controlUnderlayBody struct{ closed bool }

func (*controlUnderlayBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *controlUnderlayBody) Close() error           { b.closed = true; return nil }

func TestControlUnderlayRejectsUnauthorizedRequestsBeforeDial(t *testing.T) {
	origins := []string{"https://control.example"}
	client, err := newControlUnderlayHTTPClient(origins, 51820, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	origins[0] = "https://other.example"
	transport := client.Transport.(*controlUnderlayTransport)
	var dials atomic.Int32
	transport.base.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	for _, tc := range []struct{ name, target, host string }{
		{"other origin", "https://other.example/report", ""},
		{"plain HTTP", "http://control.example/report", ""},
		{"userinfo", "https://synthetic:secret@control.example/report", ""},
		{"fragment", "https://control.example/report#fragment", ""},
		{"Host override", "https://control.example/report", "other.example"},
		{"port override", "https://control.example:444/report", ""},
		{"opaque URL", "https:control.example/report", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, parseErr := url.Parse(tc.target)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			body := &controlUnderlayBody{}
			_, requestErr := transport.RoundTrip(&http.Request{Method: http.MethodPost, URL: u, Host: tc.host, Body: body})
			if requestErr == nil || !body.closed || dials.Load() != 0 {
				t.Fatalf("rejected request: error=%v closed=%v dials=%d", requestErr, body.closed, dials.Load())
			}
			if strings.Contains(requestErr.Error(), "secret") {
				t.Fatal("error exposed URL credentials")
			}
		})
	}
}

func TestControlUnderlayUsesMarkedTLSAndDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/report" || r.URL.Query().Get("page") != "1" {
			t.Error("request path or query changed")
		}
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer server.Close()
	var marks atomic.Int32
	client, err := newControlUnderlayHTTPClient([]string{server.URL}, 51820, func(_ syscall.RawConn, mark uint32) error {
		if mark != 51820 {
			return errors.New("wrong mark")
		}
		marks.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	transport := client.Transport.(*controlUnderlayTransport)
	if transport.base.Proxy != nil || transport.base.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Fatal("marked control client lost proxy or TLS policy")
	}
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport.base.TLSClientConfig.RootCAs = pool
	response, err := client.Get(server.URL + "/report?page=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusFound || redirected.Load() != 0 || marks.Load() == 0 {
		t.Fatalf("status=%d redirected=%d marks=%d", response.StatusCode, redirected.Load(), marks.Load())
	}
}

func TestControlUnderlayMarkFailureHasNoFallback(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	markErr := errors.New("mark rejected")
	client, err := newControlUnderlayHTTPClient([]string{server.URL}, 51820, func(syscall.RawConn, uint32) error { return markErr })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	_, err = client.Get(server.URL)
	if !errors.Is(err, markErr) || requests.Load() != 0 {
		t.Fatalf("error=%v requests=%d", err, requests.Load())
	}
}

func TestControlUnderlayRequiresValidOrigins(t *testing.T) {
	for _, origins := range [][]string{nil, {"http://control.example"}, {"https://control.example/path"}, {"https://control.example", "https://other.example?query"}} {
		client, err := newControlUnderlayHTTPClient(origins, 0, nil)
		if err == nil || client != nil {
			t.Fatalf("accepted invalid origins: %v", origins)
		}
	}
}
