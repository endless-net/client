package publicationproxy

import (
	"crypto/tls"
	"net"
	"sync"

	api "github.com/endless-net/management/api/v1"
)

// Only sockets accepted on the device-bound listener carry a peer identity.
// The peer address table belongs to this listener generation and is immutable.
type overlayConn struct {
	net.Conn
	peer *api.PublicationPeer
}

func authenticatedPeer(conn net.Conn) *api.PublicationPeer {
	if secure, ok := conn.(*tls.Conn); ok {
		conn = secure.NetConn()
	}
	if c, ok := conn.(*overlayConn); ok {
		return c.peer
	}
	return nil
}

type acceptResult struct {
	conn net.Conn
	err  error
}

type proxyListeners struct {
	public, overlay net.Listener
	results         chan acceptResult
	done            chan struct{}
	once            sync.Once
}

func combineListeners(public, overlay net.Listener, peers map[string]*api.PublicationPeer) *proxyListeners {
	l := &proxyListeners{public: public, overlay: overlay, results: make(chan acceptResult), done: make(chan struct{})}
	go l.accept(public, nil)
	go l.accept(overlay, peers)
	return l
}

func (l *proxyListeners) accept(listener net.Listener, peers map[string]*api.PublicationPeer) {
	for {
		c, err := listener.Accept()
		if err == nil && peers != nil {
			remote, ok := c.RemoteAddr().(*net.TCPAddr)
			if !ok || peers[remote.IP.String()] == nil {
				_ = c.Close()
				continue
			}
			c = &overlayConn{c, peers[remote.IP.String()]}
		}
		select {
		case l.results <- acceptResult{c, err}:
		case <-l.done:
			if c != nil {
				_ = c.Close()
			}
			return
		}
		if err != nil {
			_ = l.Close()
			return
		}
	}
}

func (l *proxyListeners) Addr() net.Addr { return l.public.Addr() }
func (l *proxyListeners) Close() error {
	l.once.Do(func() { close(l.done); _ = l.public.Close(); _ = l.overlay.Close() })
	return nil
}

func (l *proxyListeners) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case v := <-l.results:
		return v.conn, v.err
	}
}
