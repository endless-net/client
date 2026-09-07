package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/stunclient"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	"github.com/tailscale/wireguard-go/conn"
)

// MagicBind is the single UDP transport shared by wireguard-go and
// STUN endpoint discovery. Only STUN replies for transactions started by this
// bind are intercepted; every other datagram is delivered to wireguard-go.
type MagicBind struct {
	mu               sync.RWMutex
	session          *magicBindSession
	waiters          map[stunclient.TransactionID]*magicBindSTUNWaiter
	pathKeys         map[[32]byte][32]byte
	pathWaiters      map[[16]byte]magicBindPathProbeWaiter
	pathLocalPublic  [32]byte
	pathProbeEnabled bool
}

type magicBindSession struct {
	port uint16
	v4   *net.UDPConn
	v6   *net.UDPConn
	rx4  chan magicBindDatagram
	rx6  chan magicBindDatagram
	done chan struct{}
	once sync.Once
}

type magicBindDatagram struct {
	payload  []byte
	endpoint *magicBindEndpoint
}

type magicBindSTUNWaiter struct {
	remote *net.UDPAddr
	result chan *net.UDPAddr
}

type magicBindEndpoint struct {
	dst netip.AddrPort
}

var (
	_ conn.Bind     = (*MagicBind)(nil)
	_ conn.Endpoint = (*magicBindEndpoint)(nil)
)

func NewMagicBind() *MagicBind {
	return &MagicBind{
		waiters:     map[stunclient.TransactionID]*magicBindSTUNWaiter{},
		pathKeys:    map[[32]byte][32]byte{},
		pathWaiters: map[[16]byte]magicBindPathProbeWaiter{},
	}
}

func (b *MagicBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}

	session, err := openMagicBindSession(port)
	if err != nil {
		return nil, 0, err
	}
	b.session = session
	var receive []conn.ReceiveFunc
	if session.v4 != nil {
		receive = append(receive, magicBindReceiveFunc(session, session.rx4))
		go b.readLoop(session, session.v4, session.rx4)
	}
	if session.v6 != nil {
		receive = append(receive, magicBindReceiveFunc(session, session.rx6))
		go b.readLoop(session, session.v6, session.rx6)
	}
	return receive, session.port, nil
}

func openMagicBindSession(requested uint16) (*magicBindSession, error) {
	var lastErr error
	for attempt := 0; attempt < 100; attempt++ {
		port := int(requested)
		v4, err4 := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
		if v4 != nil {
			port = v4.LocalAddr().(*net.UDPAddr).Port
		}
		v6, err6 := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6zero, Port: port})
		if v4 == nil && v6 != nil {
			port = v6.LocalAddr().(*net.UDPAddr).Port
		}
		if v4 != nil || v6 != nil {
			// With an automatically selected port, retry until both address
			// families can use the same port when both are available.
			if requested == 0 && v4 != nil && v6 == nil && err6 != nil && isAddressInUse(err6) {
				_ = v4.Close()
				lastErr = err6
				continue
			}
			return &magicBindSession{
				port: uint16(port),
				v4:   v4,
				v6:   v6,
				rx4:  make(chan magicBindDatagram, 256),
				rx6:  make(chan magicBindDatagram, 256),
				done: make(chan struct{}),
			}, nil
		}
		lastErr = errors.Join(err4, err6)
		if requested != 0 || !isAddressInUse(lastErr) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no UDP address family is available")
	}
	return nil, fmt.Errorf("open shared WireGuard/STUN UDP socket: %w", lastErr)
}

func isAddressInUse(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "only one usage of each socket address")
}

func magicBindReceiveFunc(session *magicBindSession, packets <-chan magicBindDatagram) conn.ReceiveFunc {
	return func(bufs [][]byte, sizes []int, endpoints []conn.Endpoint) (int, error) {
		select {
		case <-session.done:
			return 0, net.ErrClosed
		case packet := <-packets:
			if len(bufs) == 0 || len(sizes) == 0 || len(endpoints) == 0 {
				return 0, errors.New("wireguard receive batch is empty")
			}
			sizes[0] = copy(bufs[0], packet.payload)
			endpoints[0] = packet.endpoint
			return 1, nil
		}
	}
}

func (b *MagicBind) readLoop(session *magicBindSession, socket *net.UDPConn, wireGuard chan<- magicBindDatagram) {
	buffer := make([]byte, 64*1024)
	for {
		n, remote, err := socket.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		payload := append([]byte(nil), buffer[:n]...)
		if b.dispatchSTUN(payload, remote) {
			continue
		}
		if b.dispatchPathProbe(payload, remote, socket) {
			continue
		}
		addrPort := unmapAddrPort(remote.AddrPort())
		select {
		case wireGuard <- magicBindDatagram{payload: payload, endpoint: &magicBindEndpoint{dst: addrPort}}:
		case <-session.done:
			return
		}
	}
}

func (b *MagicBind) dispatchSTUN(payload []byte, remote *net.UDPAddr) bool {
	txID, ok := stunclient.TransactionIDFromMessage(payload)
	if !ok {
		return false
	}
	b.mu.RLock()
	waiter := b.waiters[txID]
	b.mu.RUnlock()
	if waiter == nil {
		return false
	}
	// Invalid replies must not occupy the result slot or terminate discovery.
	if remote == nil || remote.Port != waiter.remote.Port || remote.Zone != waiter.remote.Zone || !remote.IP.Equal(waiter.remote.IP) {
		return true
	}
	mapped, err := stunclient.ParseBindingResponse(payload, txID)
	if err != nil {
		return true
	}
	select {
	case waiter.result <- mapped:
	default:
	}
	return true
}

func (b *MagicBind) Close() error {
	b.mu.Lock()
	session := b.session
	b.session = nil
	b.pathProbeEnabled = false
	b.pathLocalPublic = [32]byte{}
	clear(b.pathKeys)
	clear(b.pathWaiters)
	b.mu.Unlock()
	if session == nil {
		return nil
	}
	session.once.Do(func() {
		close(session.done)
	})
	var err4, err6 error
	if session.v4 != nil {
		err4 = session.v4.Close()
	}
	if session.v6 != nil {
		err6 = session.v6.Close()
	}
	return errors.Join(err4, err6)
}

func (b *MagicBind) SetMark(mark uint32) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.session == nil {
		return net.ErrClosed
	}
	return errors.Join(setMagicBindSocketMark(b.session.v4, mark), setMagicBindSocketMark(b.session.v6, mark))
}

func (b *MagicBind) Send(bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	typed, ok := endpoint.(*magicBindEndpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}
	if offset < 0 {
		return errors.New("wireguard send offset is negative")
	}
	b.mu.RLock()
	session := b.session
	b.mu.RUnlock()
	if session == nil {
		return net.ErrClosed
	}
	socket := session.v4
	if typed.dst.Addr().Is6() {
		socket = session.v6
	}
	if socket == nil {
		return fmt.Errorf("UDP address family for %s is unavailable", typed.dst)
	}
	remote := net.UDPAddrFromAddrPort(typed.dst)
	for _, buf := range bufs {
		if offset > len(buf) {
			return errors.New("wireguard send offset exceeds packet length")
		}
		if _, err := socket.WriteToUDP(buf[offset:], remote); err != nil {
			return err
		}
	}
	return nil
}

func (b *MagicBind) ParseEndpoint(value string) (conn.Endpoint, error) {
	addr, err := net.ResolveUDPAddr("udp", strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	addrPort := addr.AddrPort()
	addrPort = unmapAddrPort(addrPort)
	if !addrPort.IsValid() || addrPort.Port() == 0 {
		return nil, fmt.Errorf("WireGuard endpoint %q is invalid", value)
	}
	return &magicBindEndpoint{dst: addrPort}, nil
}

func unmapAddrPort(addrPort netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(addrPort.Addr().Unmap(), addrPort.Port())
}

func (*MagicBind) BatchSize() int { return 1 }

func (e *magicBindEndpoint) ClearSrc()           {}
func (e *magicBindEndpoint) SrcToString() string { return "" }
func (e *magicBindEndpoint) DstToString() string { return e.dst.String() }
func (e *magicBindEndpoint) DstToBytes() []byte  { value, _ := e.dst.MarshalBinary(); return value }
func (e *magicBindEndpoint) DstIP() netip.Addr   { return e.dst.Addr() }
func (e *magicBindEndpoint) SrcIP() netip.Addr   { return netip.Addr{} }

func (b *MagicBind) LocalPort() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.session == nil {
		return 0
	}
	return int(b.session.port)
}

func (b *MagicBind) LoopbackEndpoint() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.session == nil {
		return ""
	}
	if b.session.v4 != nil {
		return net.JoinHostPort("127.0.0.1", fmt.Sprint(b.session.port))
	}
	if b.session.v6 != nil {
		return net.JoinHostPort("::1", fmt.Sprint(b.session.port))
	}
	return ""
}

// CheckSTUN queries every configured STUN server through the same socket used
// for WireGuard traffic.
func (b *MagicBind) CheckSTUN(ctx context.Context, endpoints []clientapi.STUNEndpoint, timeout time.Duration) []STUNCheckResult {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	results := make([]STUNCheckResult, len(endpoints))
	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		i, endpoint := i, endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			endpoint.ID = strings.TrimSpace(endpoint.ID)
			endpoint.Addr = strings.TrimSpace(endpoint.Addr)
			result := STUNCheckResult{ID: endpoint.ID, Addr: endpoint.Addr}
			started := time.Now()
			if endpoint.Addr == "" {
				result.Error = "STUN endpoint address is required"
				results[i] = result
				return
			}
			mapped, err := b.querySTUN(ctx, endpoint.Addr, timeout)
			result.Duration = time.Since(started)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Reachable = true
				result.MappedAddress = mapped.String()
			}
			results[i] = result
		}()
	}
	wg.Wait()
	return results
}

func (b *MagicBind) querySTUN(ctx context.Context, serverAddr string, timeout time.Duration) (*net.UDPAddr, error) {
	remote, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return nil, err
	}
	request, txID, err := stunclient.BuildBindingRequest()
	if err != nil {
		return nil, err
	}
	waiter := &magicBindSTUNWaiter{remote: remote, result: make(chan *net.UDPAddr, 1)}
	b.mu.Lock()
	session := b.session
	if session == nil {
		b.mu.Unlock()
		return nil, net.ErrClosed
	}
	socket := session.v4
	if remote.IP.To4() == nil {
		socket = session.v6
	}
	if socket == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("UDP address family for %s is unavailable", serverAddr)
	}
	b.waiters[txID] = waiter
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiters, txID)
		b.mu.Unlock()
	}()
	if _, err := socket.WriteToUDP(request, remote); err != nil {
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.done:
		return nil, net.ErrClosed
	case <-timer.C:
		return nil, fmt.Errorf("STUN query to %s timed out", serverAddr)
	case mapped := <-waiter.result:
		return mapped, nil
	}
}
