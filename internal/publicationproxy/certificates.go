package publicationproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

type certificateRoutes interface {
	serverRoutes
	CertificateNames() []string
}

type managedHost struct {
	cert, challenge *tls.Certificate
	retry           time.Time
	failures        int
	cancel          context.CancelFunc
}

// Certificates owns private keys for the current proxy. Per-host work runs
// independently of handshakes so a CA can connect for its TLS-ALPN challenge.
type Certificates struct {
	mu         sync.Mutex
	accountMu  sync.Mutex
	registered bool
	register   func(context.Context) error
	ca         certificateAuthority
	routes     certificateRoutes
	cache      autocert.Cache
	roots      *x509.CertPool
	now        func() time.Time
	hosts      map[string]*managedHost
	jobs       chan struct{}
	workers    sync.WaitGroup
}

func NewManagedServer(routes certificateRoutes, account ACMEAccount, cache autocert.Cache, dial func(context.Context, netip.AddrPort) (net.Conn, error), peer func(net.Conn) *api.PublicationPeer, now func() time.Time) (*http.Server, *Certificates, error) {
	if account.Client == nil || account.Client.Key == nil || routes == nil || cache == nil || now == nil {
		return nil, nil, connectivity.ErrConfig
	}
	manager := &Certificates{register: account.Register, ca: account.Client, routes: routes, cache: cache, now: now, hosts: map[string]*managedHost{}, jobs: make(chan struct{}, 4)}
	server, err := NewServer(routes, manager.GetCertificate, dial, peer, now)
	if err != nil {
		return nil, nil, err
	}
	return server, manager, nil
}

func (c *Certificates) GetCertificate(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := strings.ToLower(h.ServerName)
	if !c.routes.AllowsCertificate(host) {
		return nil, connectivity.ErrPublication
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.hosts[host]
	if state == nil {
		return nil, connectivity.ErrPublication
	}
	if len(h.SupportedProtos) == 1 && h.SupportedProtos[0] == acme.ALPNProto {
		if state.challenge != nil {
			return state.challenge, nil
		}
		return nil, connectivity.ErrPublication
	}
	if state.cert == nil || !c.now().Before(state.cert.Leaf.NotAfter) {
		return nil, connectivity.ErrPublication
	}
	return state.cert, nil
}

func (c *Certificates) parse(host string, bundle []byte) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(bundle, bundle)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != host || len(leaf.IPAddresses)+len(leaf.URIs)+len(leaf.EmailAddresses) != 0 {
		return nil, connectivity.ErrPublication
	}
	inter := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		issuer, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		inter.AddCert(issuer)
	}
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: host, CurrentTime: c.now(), Roots: c.roots, Intermediates: inter})
	if err != nil {
		return nil, err
	}
	pair.Leaf = leaf
	return &pair, nil
}

func renewAt(leaf *x509.Certificate) time.Time {
	return leaf.NotAfter.Add(-min(30*24*time.Hour, leaf.NotAfter.Sub(leaf.NotBefore)/3))
}

func (c *Certificates) ensureAccount(ctx context.Context) error {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	if !c.registered {
		if err := c.register(ctx); err != nil {
			return err
		}
		c.registered = true
	}
	return nil
}

func (c *Certificates) obtain(ctx context.Context, host string, state *managedHost) (*tls.Certificate, error) {
	cached, err := c.cache.Get(ctx, host)
	if err == nil {
		cert, err := c.parse(host, cached)
		if err == nil {
			c.mu.Lock()
			if c.hosts[host] == state {
				state.cert = cert
			}
			c.mu.Unlock()
			if c.now().Before(renewAt(cert.Leaf)) {
				return cert, nil
			}
		}
	} else if !errors.Is(err, autocert.ErrCacheMiss) {
		return nil, err
	}
	if err = c.ensureAccount(ctx); err != nil {
		return nil, err
	}
	allowed := func(host string) bool { return ctx.Err() == nil && c.routes.AllowsCertificate(host) }
	challenge := func(v *tls.Certificate) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.hosts[host] != state || ctx.Err() != nil {
			return connectivity.ErrPublication
		}
		state.challenge = v
		return nil
	}
	data, err := issueCertificate(ctx, c.ca, host, allowed, challenge)
	if err != nil {
		return nil, err
	}
	cert, err := c.parse(host, data)
	if err != nil {
		return nil, err
	}
	if !allowed(host) {
		return nil, connectivity.ErrPublication
	}
	if err = c.cache.Put(ctx, host, data); err != nil {
		return nil, err
	}
	return cert, nil
}

func (c *Certificates) reconcile(ctx context.Context) {
	active := map[string]bool{}
	for _, host := range c.routes.CertificateNames() {
		if c.routes.AllowsCertificate(host) {
			active[host] = true
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for host, state := range c.hosts {
		if !active[host] {
			if state.cancel != nil {
				state.cancel()
			}
			delete(c.hosts, host)
		}
	}
	for host := range active {
		state := c.hosts[host]
		if state == nil {
			state = new(managedHost)
			c.hosts[host] = state
		}
		if state.cancel != nil || c.now().Before(state.retry) {
			continue
		}
		select {
		case c.jobs <- struct{}{}:
		default:
			continue
		}
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		state.cancel = cancel
		c.workers.Add(1)
		go func() {
			defer c.workers.Done()
			defer func() { <-c.jobs }()
			defer cancel()
			cert, err := c.obtain(work, host, state)
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.hosts[host] != state {
				return
			}
			state.cancel = nil
			state.challenge = nil
			if err != nil {
				state.failures = min(6, state.failures+1)
				state.retry = c.now().Add(time.Minute * (1 << (state.failures - 1)))
				return
			}
			state.cert = cert
			state.retry = renewAt(cert.Leaf)
			state.failures = 0
		}()
	}
}

// Run performs issuance and scheduled renewal while the proxy is active. It
// cancels outstanding orders and forgets in-memory credentials on shutdown.
func (c *Certificates) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		c.reconcile(ctx)
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
	c.mu.Lock()
	for host, s := range c.hosts {
		if s.cancel != nil {
			s.cancel()
		}
		delete(c.hosts, host)
	}
	c.mu.Unlock()
	c.workers.Wait()
}
