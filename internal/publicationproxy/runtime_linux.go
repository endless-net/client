package publicationproxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	contract "github.com/endless-net/coordinator/api/enrollment"
	api "github.com/endless-net/management/api/v1"
	rpc "github.com/endless-net/management/api/v1/managementapiconnect"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sys/unix"
)

// LinuxRuntime is installed only in the dedicated proxy process. Its node
// session supplies the verified lease and the interface it actually owns.
type LinuxRuntime struct {
	PublicAddress netip.AddrPort
	Executor      *api.PublicationExecutor
	Management    rpc.PublicationExecutorServiceClient
	Account       ACMEAccount
	Cache         autocert.Cache
	mu            sync.Mutex
	worker        *LinuxWorker
}

type LinuxWorker struct {
	done    chan struct{}
	err     error
	cancel  context.CancelFunc
	address string
	peers   map[string]*api.PublicationPeer
}

// Before replacing the native map, discard queued TCP handshakes and open
// sockets if their source addresses could acquire different owners.
func (w *LinuxWorker) Matches(value connectivity.Map) bool {
	s, err := value.Snapshot()
	return err == nil && s.Node.AssignedIP == w.address && w.matchesPeers(s.Peers)
}

func (w *LinuxWorker) matchesPeers(peers []contract.Peer) bool {
	if len(peers) != len(w.peers) {
		return false
	}
	for _, peer := range peers {
		if len(peer.AllowedIPs) != 1 {
			return false
		}
		p, err := netip.ParsePrefix(peer.AllowedIPs[0])
		id := w.peers[p.Addr().String()]
		if err != nil || id == nil || id.NodeId != peer.ID || id.PublicKey != peer.PublicKey {
			return false
		}
	}
	return true
}

func (w *LinuxWorker) Err() error {
	select {
	case <-w.done:
		if w.err == nil {
			return net.ErrClosed
		}
		return w.err
	default:
		return nil
	}
}

func (w *LinuxWorker) Stop(ctx context.Context) error {
	w.cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		if errors.Is(w.err, context.Canceled) {
			return nil
		}
		return w.err
	}
}

func bindOverlay(index int) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		if index <= 0 {
			return connectivity.ErrConfig
		}
		var bindErr error
		err := raw.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BINDTOIFINDEX, index)
		})
		return errors.Join(err, bindErr)
	}
}

func (p *LinuxRuntime) Start(ctx context.Context, lease *connectivity.Lease, index int, address netip.Addr) (*LinuxWorker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.drained() {
		return nil, connectivity.ErrPublication
	}
	if p.Management == nil || !p.PublicAddress.IsValid() || !p.PublicAddress.Addr().IsGlobalUnicast() || p.PublicAddress.Port() == 0 || index <= 0 || !address.Is4() || p.PublicAddress.Addr() == address {
		return nil, connectivity.ErrConfig
	}
	routes, err := connectivity.NewPublicationRoutes(p.Executor, lease, time.Now)
	if err != nil {
		return nil, err
	}
	snapshot, err := lease.CurrentMap().Snapshot()
	if err != nil || !lease.Permit(false) || snapshot.Network.AccountID != p.Executor.AccountId || snapshot.Network.ID != p.Executor.NetworkId || snapshot.Node.ID != p.Executor.NodeId || snapshot.Node.AssignedIP != address.String() {
		return nil, connectivity.ErrPublication
	}
	dialer := &net.Dialer{LocalAddr: net.TCPAddrFromAddrPort(netip.AddrPortFrom(address, 0)), Control: bindOverlay(index)}
	dial := func(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", target.String())
	}
	server, certs, err := NewManagedServer(routes, p.Account, p.Cache, dial, authenticatedPeer, time.Now)
	if err != nil {
		return nil, err
	}
	var listen net.ListenConfig
	listener, err := listen.Listen(ctx, "tcp", p.PublicAddress.String())
	if err != nil {
		return nil, err
	}
	listen.Control = bindOverlay(index)
	overlay, err := listen.Listen(ctx, "tcp4", netip.AddrPortFrom(address, p.PublicAddress.Port()).String())
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	peers := make(map[string]*api.PublicationPeer, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		prefix, err := netip.ParsePrefix(peer.AllowedIPs[0]) // verified unique /32
		if err != nil {
			_ = overlay.Close()
			_ = listener.Close()
			return nil, connectivity.ErrPublication
		}
		peers[prefix.Addr().String()] = &api.PublicationPeer{NodeId: peer.ID, PublicKey: peer.PublicKey}
	}
	both := combineListeners(listener, overlay, peers)
	run, cancel := context.WithCancel(ctx)
	worker := &LinuxWorker{done: make(chan struct{}), cancel: cancel, address: address.String(), peers: peers}
	p.worker = worker
	go func() {
		defer close(worker.done)
		defer cancel()
		worker.err = Serve(run, both, server, certs, routes, p.Management, dial)
	}()
	return worker, nil
}
