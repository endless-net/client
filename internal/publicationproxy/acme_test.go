package publicationproxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

type managedRoutesFixture struct{ certRoutes }

func (*managedRoutesFixture) CertificateNames() []string { return []string{"docs.example.org"} }

type acmeFixture struct {
	host              string
	clock             func() time.Time
	root              *x509.Certificate
	key               *ecdsa.PrivateKey
	accepted          func()
	orders, finalized int
	fail              bool
}

func (f *acmeFixture) order() *acme.Order {
	return &acme.Order{URI: "order", AuthzURLs: []string{"auth"}, Identifiers: acme.DomainIDs(f.host), FinalizeURL: "final", Status: acme.StatusReady}
}
func (f *acmeFixture) AuthorizeOrder(_ context.Context, id []acme.AuthzID, _ ...acme.OrderOption) (*acme.Order, error) {
	f.orders++
	if len(id) != 1 || id[0].Value != f.host || f.fail {
		return nil, errors.New("order unavailable")
	}
	return f.order(), nil
}
func (f *acmeFixture) GetAuthorization(context.Context, string) (*acme.Authorization, error) {
	return &acme.Authorization{Identifier: acme.DomainIDs(f.host)[0], Status: acme.StatusPending, Challenges: []*acme.Challenge{{Type: "tls-alpn-01", Token: "test-token"}}}, nil
}
func (f *acmeFixture) TLSALPN01ChallengeCert(token, host string, _ ...acme.CertOption) (tls.Certificate, error) {
	return (&acme.Client{Key: f.key}).TLSALPN01ChallengeCert(token, host)
}
func (f *acmeFixture) Accept(_ context.Context, ch *acme.Challenge) (*acme.Challenge, error) {
	f.accepted()
	return ch, nil
}
func (f *acmeFixture) WaitOrder(context.Context, string) (*acme.Order, error) { return f.order(), nil }
func (f *acmeFixture) CreateOrderCert(_ context.Context, url string, raw []byte, chain bool) ([][]byte, string, error) {
	csr, err := x509.ParseCertificateRequest(raw)
	if err != nil {
		return nil, "", err
	}
	if url != "final" || !chain || csr.CheckSignature() != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != f.host {
		return nil, "", errors.New("unexpected CSR")
	}
	f.finalized++
	leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(f.finalized)), DNSNames: csr.DNSNames, NotBefore: f.clock(), NotAfter: f.clock().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, f.root, csr.PublicKey, f.key)
	return [][]byte{der}, "certificate", err
}

func TestACMEIssuanceRenewalCacheAndWithdrawal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caCert := &x509.Certificate{SerialNumber: big.NewInt(91), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, caCert, caCert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	clock := func() time.Time { return now }
	r := &managedRoutesFixture{certRoutes{enabled: true}}
	const host = "docs.example.org"
	ca := &acmeFixture{host: host, key: key, root: root, clock: clock}
	cache := autocert.DirCache(t.TempDir())
	registrations := 0
	manager := func() *Certificates {
		return &Certificates{routes: r, cache: cache, roots: roots, now: clock, hosts: map[string]*managedHost{}, jobs: make(chan struct{}, 1), ca: ca, register: func(context.Context) error { registrations++; return nil }}
	}
	c := manager()
	hello := &tls.ClientHelloInfo{ServerName: host}
	challenge := &tls.ClientHelloInfo{ServerName: host, SupportedProtos: []string{acme.ALPNProto}}
	ca.accepted = func() {
		if _, err := c.GetCertificate(challenge); err != nil {
			t.Error("challenge unavailable", err)
		}
	}
	cycle := func() { c.reconcile(t.Context()); c.workers.Wait() }
	cycle()
	cert, err := c.GetCertificate(hello)
	if err != nil || cert.Leaf.SerialNumber.Int64() != 1 || registrations != 1 {
		t.Fatal(cert, err, registrations)
	}
	if _, err := c.GetCertificate(challenge); err == nil {
		t.Fatal("challenge survived issuance")
	}
	c = manager()
	cycle()
	cert, err = c.GetCertificate(hello)
	if err != nil || cert.Leaf.SerialNumber.Int64() != 1 || ca.orders != 1 {
		t.Fatal("cold cache", err, ca.orders)
	}
	now = renewAt(cert.Leaf)
	ca.fail = true
	cycle()
	cert, err = c.GetCertificate(hello)
	if err != nil || cert.Leaf.SerialNumber.Int64() != 1 {
		t.Fatal("early renewal failure discarded current leaf", err)
	}
	cycle()
	if ca.orders != 2 {
		t.Fatal("missed failure backoff", ca.orders)
	}
	ca.fail = false
	now = now.Add(time.Minute)
	cycle()
	cert, err = c.GetCertificate(hello)
	if err != nil || cert.Leaf.SerialNumber.Int64() != 2 {
		t.Fatal("renewal", err)
	}
	now = renewAt(cert.Leaf)
	ca.accepted = func() { r.enabled = false }
	cycle()
	if ca.finalized != 2 {
		t.Fatal("withdrawn order finalized", ca.finalized)
	}
	if _, err = c.GetCertificate(hello); err != connectivity.ErrPublication {
		t.Fatal("withdrawn certificate served", err)
	}
	cycle()
	if len(c.hosts) != 0 {
		t.Fatal("retained withdrawn host")
	}
}
