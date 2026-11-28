package publicationproxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
	"golang.org/x/crypto/acme"
)

// ACMEAccount uses a persistent operator-configured account key. The caller
// supplies the terms policy explicitly for first registration.
type ACMEAccount struct {
	Client      *acme.Client
	Contact     []string
	AcceptTerms func(string) bool
}

func (a ACMEAccount) Register(ctx context.Context) error {
	if a.Client == nil || a.Client.Key == nil {
		return connectivity.ErrConfig
	}
	account, err := a.Client.GetReg(ctx, "")
	if err == nil {
		if account == nil || account.Status != acme.StatusValid {
			return connectivity.ErrPublication
		}
		return nil
	}
	if !errors.Is(err, acme.ErrNoAccount) {
		return err
	}
	if a.AcceptTerms == nil {
		return connectivity.ErrConfig
	}
	account, err = a.Client.Register(ctx, &acme.Account{Contact: a.Contact}, a.AcceptTerms)
	if errors.Is(err, acme.ErrAccountAlreadyExists) {
		account, err = a.Client.GetReg(ctx, "")
	}
	if err != nil {
		return err
	}
	if account == nil || account.Status != acme.StatusValid {
		return connectivity.ErrPublication
	}
	return nil
}

type certificateAuthority interface {
	AuthorizeOrder(context.Context, []acme.AuthzID, ...acme.OrderOption) (*acme.Order, error)
	GetAuthorization(context.Context, string) (*acme.Authorization, error)
	TLSALPN01ChallengeCert(string, string, ...acme.CertOption) (tls.Certificate, error)
	Accept(context.Context, *acme.Challenge) (*acme.Challenge, error)
	WaitOrder(context.Context, string) (*acme.Order, error)
	CreateOrderCert(context.Context, string, []byte, bool) ([][]byte, string, error)
}

type publishChallenge func(*tls.Certificate) error

func issueCertificate(ctx context.Context, ca certificateAuthority, host string, allowed func(string) bool, challenge publishChallenge) ([]byte, error) {
	ids := acme.DomainIDs(host)
	if !allowed(host) {
		return nil, connectivity.ErrPublication
	}
	order, err := ca.AuthorizeOrder(ctx, ids)
	if err != nil {
		return nil, err
	}
	matches := func(o *acme.Order) bool {
		return o != nil && o.URI != "" && len(o.Identifiers) == 1 && o.Identifiers[0] == ids[0]
	}
	if !matches(order) || len(order.AuthzURLs) != 1 {
		return nil, connectivity.ErrPublication
	}
	auth, err := ca.GetAuthorization(ctx, order.AuthzURLs[0])
	if err != nil {
		return nil, err
	}
	if auth == nil || auth.Identifier != ids[0] || auth.Wildcard || !allowed(host) {
		return nil, connectivity.ErrPublication
	}
	switch auth.Status {
	case acme.StatusPending:
		var ch *acme.Challenge
		for _, candidate := range auth.Challenges {
			if candidate != nil && candidate.Type == "tls-alpn-01" {
				ch = candidate
				break
			}
		}
		if ch == nil {
			return nil, connectivity.ErrPublication
		}
		cert, err := ca.TLSALPN01ChallengeCert(ch.Token, host)
		if err != nil {
			return nil, err
		}
		if err = challenge(&cert); err != nil {
			return nil, err
		}
		defer func() { _ = challenge(nil) }()
		if _, err = ca.Accept(ctx, ch); err != nil {
			return nil, err
		}
	case acme.StatusValid:
	default:
		return nil, connectivity.ErrPublication
	}
	ready, err := ca.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, err
	}
	if !matches(ready) || ready.URI != order.URI || ready.Status != acme.StatusReady || ready.FinalizeURL == "" || !allowed(host) {
		return nil, connectivity.ErrPublication
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{host}}, key)
	if err != nil {
		return nil, err
	}
	chain, _, err := ca.CreateOrderCert(ctx, ready.FinalizeURL, csr, true)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 || !allowed(host) {
		return nil, connectivity.ErrPublication
	}
	var bundle []byte
	for _, der := range chain {
		bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return append(bundle, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw})...), nil
}
