package testwireguard

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sync"

	"github.com/tailscale/wireguard-go/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

// referenceStack owns the test peer's protocol workers and packet queue. Read
// consumes the link's synchronized queue directly: no notification callback can
// remain blocked sending into a second channel while Close tears the stack down.
// WireGuard and TCP/IP remain independent third-party protocol implementations.
type referenceStack struct {
	stack  *stack.Stack
	link   *channel.Endpoint
	events chan tun.Event
	once   sync.Once
	mu     sync.RWMutex
	closed bool
}

var _ tun.Device = (*referenceStack)(nil)

func newReferenceStack(address netip.Addr) (*referenceStack, error) {
	s := &referenceStack{
		stack: stack.New(stack.Options{
			NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
			TransportProtocols: []stack.TransportProtocolFactory{
				tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6,
			},
			HandleLocal: true,
		}),
		link: channel.New(1024, 1280, ""), events: make(chan tun.Event, 1),
	}
	fail := func(err tcpip.Error) (*referenceStack, error) {
		_ = s.Close()
		return nil, fmt.Errorf("reference stack setup: %s", err)
	}
	sack := tcpip.TCPSACKEnabled(true)
	if err := s.stack.SetTransportProtocolOption(tcp.ProtocolNumber, &sack); err != nil {
		return fail(err)
	}
	if err := s.stack.CreateNIC(1, s.link); err != nil {
		return fail(err)
	}
	protocol, subnet := ipv4.ProtocolNumber, header.IPv4EmptySubnet
	if address.Is6() {
		protocol, subnet = ipv6.ProtocolNumber, header.IPv6EmptySubnet
	}
	if err := s.stack.AddProtocolAddress(1, tcpip.ProtocolAddress{
		Protocol: protocol, AddressWithPrefix: tcpip.AddrFromSlice(address.AsSlice()).WithPrefix(),
	}, stack.AddressProperties{}); err != nil {
		return fail(err)
	}
	s.stack.AddRoute(tcpip.Route{Destination: subnet, NIC: 1})
	s.events <- tun.EventUp
	return s, nil
}

func (*referenceStack) Name() (string, error)      { return "reference", nil }
func (*referenceStack) File() *os.File             { return nil }
func (*referenceStack) MTU() (int, error)          { return 1280, nil }
func (*referenceStack) BatchSize() int             { return 1 }
func (s *referenceStack) Events() <-chan tun.Event { return s.events }

func (s *referenceStack) Read(buffers [][]byte, sizes []int, offset int) (int, error) {
	packet := s.link.ReadContext(context.Background())
	if packet.IsNil() {
		return 0, os.ErrClosed
	}
	defer packet.DecRef()
	view := packet.ToView()
	defer view.Release()
	if view.Size() > len(buffers[0])-offset {
		return 0, io.ErrShortBuffer
	}
	sizes[0] = copy(buffers[0][offset:], view.AsSlice())
	return 1, nil
}

func (s *referenceStack) Write(buffers [][]byte, offset int) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	for i, data := range buffers {
		data = data[offset:]
		if len(data) == 0 {
			continue
		}
		protocol := header.IPv4ProtocolNumber
		if data[0]>>4 == 6 {
			protocol = header.IPv6ProtocolNumber
		} else if data[0]>>4 != 4 {
			return i, fmt.Errorf("invalid reference packet family")
		}
		packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(data)})
		s.link.InjectInbound(protocol, packet)
		packet.DecRef()
	}
	return len(buffers), nil
}

func (s *referenceStack) Close() error {
	s.once.Do(func() {
		// Stop new packet injection before aborting endpoints. The link queue
		// permits concurrent protocol writes during close and wakes Read.
		s.mu.Lock()
		s.closed = true
		s.link.Close()
		s.mu.Unlock()
		s.stack.Destroy()
		close(s.events)
	})
	return nil
}

func referenceAddress(address netip.AddrPort) (tcpip.FullAddress, tcpip.NetworkProtocolNumber) {
	protocol := ipv4.ProtocolNumber
	if address.Addr().Is6() {
		protocol = ipv6.ProtocolNumber
	}
	return tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFromSlice(address.Addr().AsSlice()), Port: address.Port()}, protocol
}

func (s *referenceStack) ListenTCPAddrPort(address netip.AddrPort) (*gonet.TCPListener, error) {
	local, protocol := referenceAddress(address)
	return gonet.ListenTCP(s.stack, local, protocol)
}

func (s *referenceStack) ListenUDPAddrPort(address netip.AddrPort) (*gonet.UDPConn, error) {
	local, protocol := referenceAddress(address)
	return gonet.DialUDP(s.stack, &local, nil, protocol)
}
