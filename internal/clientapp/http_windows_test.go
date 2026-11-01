//go:build windows

package clientapp

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/endless-net/client/internal/control"
)

func TestConfiguredNativeHTTPClientStartsNoIOAndUsesExplicitBounds(t *testing.T) {
	c, err := ParseWindows(configEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	client, err := testSignals(t).NewHTTPClient(c)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if client.Timeout != c.CallTimeout || client.Jar != nil || client.CheckRedirect(&http.Request{}, nil) != ErrSignals {
		t.Fatal("native policy changed")
	}
	observed, ok := client.Transport.(*observedHTTP)
	if !ok {
		t.Fatal("outgoing requests not observed")
	}
	transport, ok := observed.base.(*http.Transport)
	if !ok {
		t.Fatal("native transport missing")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS13 || transport.TLSClientConfig.InsecureSkipVerify || len(transport.TLSClientConfig.Certificates) != 0 || transport.TLSClientConfig.GetClientCertificate != nil || transport.MaxConnsPerHost != c.MaxConnections || transport.TLSHandshakeTimeout != c.CallTimeout || transport.ResponseHeaderTimeout != c.CallTimeout {
		t.Fatal("unbounded or unsafe native transport")
	}
	if _, err := control.NewBootstrap(client, int64(c.MaxBootstrapBytes), nil); err != nil {
		t.Fatal("native bootstrap refused configured client", err)
	}
}
