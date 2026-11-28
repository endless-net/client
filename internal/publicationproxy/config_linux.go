package publicationproxy

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	rpc "github.com/endless-net/management/api/v1/managementapiconnect"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sys/unix"
)

type proxyConfig struct {
	PublicAddress    netip.AddrPort `json:"public_address"`
	AccountID        string         `json:"account_id"`
	NetworkID        string         `json:"network_id"`
	NodeID           string         `json:"node_id"`
	ProxyID          string         `json:"proxy_id"`
	ManagementOrigin string         `json:"management_origin"`
	DirectoryURL     string         `json:"acme_directory"`
	TermsURL         string         `json:"accepted_terms_url"`
}

func decodeConfig(data []byte) (proxyConfig, error) {
	var c proxyConfig
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || c.AccountID == "" || c.NetworkID == "" || c.NodeID == "" || c.ProxyID == "" || !c.PublicAddress.Addr().IsGlobalUnicast() || c.PublicAddress.Port() == 0 {
		return c, connectivity.ErrConfig
	}
	for _, raw := range []string{c.ManagementOrigin, c.DirectoryURL, c.TermsURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (raw == c.ManagementOrigin && u.Path != "") {
			return c, connectivity.ErrConfig
		}
	}
	return c, nil
}

// OpenLinuxRuntime uses the local SPIFFE Workload API for rotating mTLS
// credentials. Only the Management workload may answer private route calls.
func OpenLinuxRuntime(ctx context.Context) (*LinuxRuntime, func(), error) {
	confDir, err := os.Lstat("/etc/endlessnet-proxy")
	if err != nil || !confDir.IsDir() || confDir.Mode().Perm()&0022 != 0 || confDir.Sys().(*syscall.Stat_t).Uid != 0 {
		return nil, nil, connectivity.ErrConfig
	}
	data, err := readProxyFile("/etc/endlessnet-proxy/config.json", 0022)
	if err != nil {
		return nil, nil, err
	}
	c, err := decodeConfig(data)
	if err != nil {
		return nil, nil, err
	}
	key, err := readProxyFile("/etc/endlessnet-proxy/acme-account.pem", 0077)
	if err != nil {
		return nil, nil, err
	}
	defer clear(key)
	block, rest := pem.Decode(key)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil, connectivity.ErrConfig
	}
	defer clear(block.Bytes)
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	signer, ok := k.(crypto.Signer)
	if err != nil || !ok {
		return nil, nil, connectivity.ErrConfig
	}
	const cache = "/var/lib/endlessnet-proxy"
	if err := os.MkdirAll(cache, 0700); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(cache)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return nil, nil, connectivity.ErrConfig
	}
	source, err := workloadapi.NewX509Source(ctx)
	if err != nil {
		return nil, nil, err
	}
	config := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(spiffeid.RequireFromString("spiffe://endlessnet.ru/service/management")))
	config.MinVersion = tls.VersionTLS13
	transport := &http.Transport{TLSClientConfig: config, ForceAttemptHTTP2: true, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &LinuxRuntime{
		PublicAddress: c.PublicAddress, Executor: &api.PublicationExecutor{AccountId: c.AccountID, NetworkId: c.NetworkID, NodeId: c.NodeID, ProxyId: c.ProxyID},
		Management: rpc.NewPublicationExecutorServiceClient(client, c.ManagementOrigin, connect.WithReadMaxBytes(1<<20), connect.WithSendMaxBytes(1<<20)),
		Account:    ACMEAccount{Client: &acme.Client{Key: signer, DirectoryURL: c.DirectoryURL}, AcceptTerms: func(terms string) bool { return terms == c.TermsURL }},
		Cache:      autocert.DirCache(cache),
	}, func() { transport.CloseIdleConnections(); _ = source.Close() }, nil
}

func readProxyFile(path string, disallowed os.FileMode) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&disallowed != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return nil, connectivity.ErrConfig
	}
	const max = 64 << 10
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if len(data) > max {
		clear(data)
		return nil, connectivity.ErrConfig
	}
	return data, err
}
