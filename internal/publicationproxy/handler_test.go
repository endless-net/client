package publicationproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
)

type routeFunc func(string, *api.PublicationPeer) (connectivity.PublicationTarget, error)

func (f routeFunc) Resolve(h string, p *api.PublicationPeer) (connectivity.PublicationTarget, error) {
	return f(h, p)
}

func TestHTTPSAdmissionPinsOverlayAndBackendHost(t *testing.T) {
	now := time.Now()
	generation, cancel := context.WithCancel(t.Context())
	defer cancel()
	addr := netip.MustParseAddrPort("100.70.0.2:8443")
	target := connectivity.PublicationTarget{Address: addr, TLSName: "docs.internal", Until: now.Add(time.Minute), Generation: generation}
	certEnd := now.Add(time.Hour)
	type requestData struct{ host, path, query, forwarded, node string }
	backend := make(chan requestData, 1)
	calls := 0
	var audit bytes.Buffer
	var resolveError error
	var dialError error
	h := Handler{Logger: slog.New(slog.NewJSONHandler(&audit, nil)), Now: func() time.Time { return now }, Identity: func(*http.Request) ConnectionIdentity { return ConnectionIdentity{CertificateUntil: certEnd} },
		Routes: routeFunc(func(host string, p *api.PublicationPeer) (connectivity.PublicationTarget, error) {
			if host != "docs.example.org" || p != nil {
				return connectivity.PublicationTarget{}, connectivity.ErrPublication
			}
			return target, resolveError
		}),
		DialOverlay: func(_ context.Context, to netip.AddrPort) (net.Conn, error) {
			if to != addr {
				return nil, connectivity.ErrPublication
			}
			calls++
			if dialError != nil {
				return nil, dialError
			}
			client, server := net.Pipe()
			go func() {
				defer func() { _ = server.Close() }()
				r, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				backend <- requestData{r.Host, r.URL.Path, r.URL.RawQuery, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Node-ID")}
				_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
			}()
			return client, nil
		}}
	request := func(host, sni string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "https://"+host+"/docs?q=a", nil)
		r.TLS = &tls.ConnectionState{ServerName: sni}
		r.Header.Set("X-Forwarded-For", "spoof")
		r.Header.Set("X-Node-ID", "spoof")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	out := request("DOCS.example.org:443", "docs.example.org")
	if out.Code != 200 || out.Body.String() != "ok" {
		t.Fatal(out.Code, out.Body.String())
	}
	want := requestData{"docs.internal", "/docs", "q=a", "", ""}
	if got := <-backend; got != want {
		t.Fatal(got)
	}
	if got := request("docs.example.org", "other.example.org").Code; got != 404 {
		t.Fatal(got)
	}
	certEnd = now
	if got := request("docs.example.org", "docs.example.org").Code; got != 404 {
		t.Fatal(got)
	}
	certEnd = now.Add(time.Hour)
	resolveError = connectivity.ErrPublicationTarget
	if got := request("docs.example.org", "docs.example.org").Code; got != 404 {
		t.Fatal(got)
	}
	resolveError = nil
	dialError = errors.New("target connection refused")
	if got := request("docs.example.org", "docs.example.org").Code; got != http.StatusBadGateway {
		t.Fatal(got)
	}
	dialError = nil
	cancel()
	if got := request("docs.example.org", "docs.example.org").Code; got == 200 {
		t.Fatal("revoked snapshot reached backend")
	}
	if calls != 2 {
		t.Fatal("overlay dials", calls)
	}
	if strings.Contains(audit.String(), "?q=") || strings.Contains(audit.String(), "spoof") {
		t.Fatal("request secrets in access log")
	}
	decoder := json.NewDecoder(&audit)
	for _, want := range []string{"forwarded", "access_denied", "access_denied", "target_unavailable", "target_unavailable", "access_denied"} {
		var row struct {
			Outcome string
			Status  int `json:"backend_status"`
		}
		if err := decoder.Decode(&row); err != nil || row.Outcome != want || (row.Status == 200) != (want == "forwarded") {
			t.Fatalf("access record %+v, expected %s: %v", row, want, err)
		}
	}
	if decoder.Decode(new(any)) != io.EOF {
		t.Fatal("extra access record")
	}
}
