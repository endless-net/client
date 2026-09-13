package testcontrol

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExplicitNativeFixtureUsesTrustedHTTPS(t *testing.T) {
	s := NewTLS(t)
	if !strings.HasPrefix(s.URL(), "https://127.0.0.1:") || len(s.TLSCertificatePEM()) == 0 {
		t.Fatal("native fixture lacks HTTPS origin or public trust material")
	}
	response, err := s.HTTP.Client().Get(s.URL())
	if err != nil {
		t.Fatal("fixture TLS handshake failed", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}
	defer transport.CloseIdleConnections()
	untrusted := &http.Client{Transport: transport, Timeout: time.Second}
	response, err = untrusted.Get(s.URL())
	if response != nil {
		_ = response.Body.Close()
	}
	var authority x509.UnknownAuthorityError
	if !errors.As(err, &authority) {
		t.Fatal("native TLS fixture did not require explicit certificate trust")
	}
}
