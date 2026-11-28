package publicationproxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

type certRoutes struct {
	routeFunc
	enabled bool
}

func (r *certRoutes) AllowsCertificate(host string) bool {
	return host == "docs.example.org" && r.enabled
}

func TestCertificateSelectionRechecksRouteAfterIssuerAndBindsLeafToConnection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"docs.example.org"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	raw, err := x509.CreateCertificate(rand.Reader, leaf, leaf, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := &tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}
	r := &certRoutes{enabled: true}
	state := new(servedCertificate)
	ctx := context.WithValue(t.Context(), connectionKey{}, state)
	issue := func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return cert, nil }
	check := func(host string, callback func(*tls.ClientHelloInfo) (*tls.Certificate, error)) error {
		_, err := selectCertificate(ctx, &tls.ClientHelloInfo{ServerName: host}, r, callback, func() time.Time { return now })
		return err
	}
	if err := check("foreign.example.org", func(*tls.ClientHelloInfo) (*tls.Certificate, error) { t.Fatal("foreign issuance"); return nil, nil }); err != connectivity.ErrPublication {
		t.Fatal(err)
	}
	if err := check("docs.example.org", func(h *tls.ClientHelloInfo) (*tls.Certificate, error) { r.enabled = false; return issue(h) }); err != connectivity.ErrPublication || !state.until.IsZero() {
		t.Fatal(err, state.until)
	}
	r.enabled = true
	if err := check("DOCS.example.org", issue); err != nil || !state.until.Equal(leaf.NotAfter) {
		t.Fatal(err, state.until)
	}
	now = leaf.NotAfter
	if err := check("docs.example.org", issue); err != connectivity.ErrPublication {
		t.Fatal(err)
	}
}
