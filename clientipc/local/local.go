// Package local binds generated v0 RPC to OS-authenticated local channels.
package local

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	clientipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
)

const (
	DefaultWindowsPipe  = `\\.\pipe\endlessnet-service`
	DefaultUnixSocket   = "/run/endlessnet/client.sock"
	DefaultDarwinSocket = "/var/run/endlessnet/client.sock"
)

type Peer struct {
	Identity      string
	Administrator bool
}

type peerContextKey struct{}

type authenticatedConn struct {
	net.Conn
	mu      sync.Mutex
	once    sync.Once
	peer    Peer
	authErr error
}

// Authenticate after the first read: Windows pipe impersonation needs an actual
// client message. ConnContext runs before the read and therefore cannot do it.
func (c *authenticatedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.once.Do(func() {
			peer, authErr := identify(c.Conn)
			c.mu.Lock()
			c.peer, c.authErr = peer, authErr
			c.mu.Unlock()
		})
		c.mu.Lock()
		authErr := c.authErr
		c.mu.Unlock()
		if authErr != nil {
			_ = c.Close()
			return 0, authErr
		}
	}
	return n, err
}

func PeerFromContext(ctx context.Context) (Peer, bool) {
	conn, ok := ctx.Value(peerContextKey{}).(*authenticatedConn)
	if !ok {
		return Peer{}, false
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.peer, conn.authErr == nil && conn.peer.Identity != ""
}

type authenticatedListener struct{ net.Listener }

func (l authenticatedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &authenticatedConn{Conn: conn}, nil
}

func Listen(endpoint string) (net.Listener, error) {
	listener, err := listen(endpoint)
	if err != nil {
		return nil, err
	}
	return authenticatedListener{listener}, nil
}

func NewServer(handler http.Handler) *http.Server {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			if peerConn, ok := conn.(*authenticatedConn); ok {
				return context.WithValue(ctx, peerContextKey{}, peerConn)
			}
			return ctx // Guard rejects unidentified connections.
		},
	}
}

type Client struct {
	clientipcconnect.ClientServiceClient
	transport *http.Transport
}

func NewClient(endpoint string) (*Client, error) {
	endpoint, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{
		Protocols: protocols,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dial(ctx, endpoint)
		},
	}
	return &Client{
		ClientServiceClient: clientipcconnect.NewClientServiceClient(
			&http.Client{Transport: transport}, "http://endlessnet.local",
			connect.WithGRPC(), connect.WithInterceptors(rpc.ClientHeaders{}),
			connect.WithReadMaxBytes(rpc.MaxResponseBytes),
			connect.WithSendMaxBytes(rpc.MaxRequestBytes),
		),
		transport: transport,
	}, nil
}

func (c *Client) Close() { c.transport.CloseIdleConnections() }

func (c *Client) Bootstrap(ctx context.Context) (*clientipc.RuntimeInfo, error) {
	response, err := c.GetRuntimeInfo(ctx, connect.NewRequest(&clientipc.GetRuntimeInfoRequest{}))
	if err != nil {
		return nil, err
	}
	info := response.Msg.GetRuntime()
	if info.GetProtocol() != rpc.Protocol || info.GetIpcVersion() != rpc.Version ||
		info.GetContractSha256() != rpc.Digest() || info.GetInstanceId() == "" {
		return nil, rpc.Error(connect.CodeFailedPrecondition, clientipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH)
	}
	return info, nil
}
