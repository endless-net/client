package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"golang.org/x/sys/unix"
)

func openLinuxBrokerSocket(ctx context.Context, mark uint32) (*net.UDPConn, error) {
	if mark == 0 {
		return nil, ErrConfiguration
	}
	config := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var opt error
		err := c.Control(func(fd uintptr) { opt = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark)) })
		return errors.Join(err, opt)
	}}
	socket, err := config.ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return socket.(*net.UDPConn), nil
}

type linuxProbeBinding struct {
	assignment  *candidateAssignment
	sourceEpoch uint64
	device      string
	local, peer netip.Addr
	port        uint16
}

type linuxProbeSocket struct {
	conn    *net.UDPConn
	binding linuxProbeBinding
	index   int
	engine  string
}

// Bind before installing the service address so the ephemeral port is already
// reserved when the host policy starts delivering candidate traffic.
func openLinuxProbeSocket(ctx context.Context, id linuxFilterIdentity, device string, a candidateAssignment) (*linuxProbeSocket, error) {
	prefix, _ := linuxPeerScope(id.owner)
	if a.lease == nil || id.mark == 0 || !linuxPeerLinkName.MatchString(device) || !strings.HasPrefix(device, prefix) || a.plan.LocalProbeIPv4 == "" || a.plan.PeerProbeIPv4 == "" {
		return nil, ErrConfiguration
	}
	local, peer, err := a.controlAddresses()
	if err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(device)
	if err != nil {
		return nil, err
	}
	config := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.BindToDevice(int(fd), device)
			if sockErr != nil {
				return
			}
			for _, opt := range [][3]int{{unix.SOL_SOCKET, unix.SO_MARK, int(id.mark)}, {unix.IPPROTO_IP, unix.IP_FREEBIND, 1}, {unix.IPPROTO_IP, unix.IP_MTU_DISCOVER, unix.IP_PMTUDISC_DO}} {
				sockErr = unix.SetsockoptInt(int(fd), opt[0], opt[1], opt[2])
				if sockErr != nil {
					return
				}
			}
		})
		if err != nil {
			return err
		}
		return sockErr
	}}
	listener, err := config.ListenPacket(ctx, "udp4", netip.AddrPortFrom(local, 0).String())
	if err != nil {
		return nil, err
	}
	conn := listener.(*net.UDPConn)
	epoch, _ := a.lease.SourceSignal()
	// Only the original pair is compared on refresh. Do not pin a complete
	// historical network map for every open service socket.
	a.mapValue = connectivity.Map{}
	return &linuxProbeSocket{conn: conn, index: iface.Index, engine: a.offer.Profile.Engine, binding: linuxProbeBinding{assignment: &a, sourceEpoch: epoch, device: device, local: local, peer: peer, port: conn.LocalAddr().(*net.UDPAddr).AddrPort().Port()}}, nil
}

// The maintenance guard receives the size of the WireGuard UDP payload;
// chargeService adds the outer IPv4 and UDP headers exactly once.
func (s *linuxProbeSocket) sendWireGuard(raw []byte, channel *assignedCipher) error {
	if s.engine != transport.WireGuard || channel == nil {
		return ErrNoAuthority
	}
	reserve := channel.controlSendGuard(raw)
	if reserve == nil {
		return ErrNoAuthority
	}
	return s.send(raw, func() error {
		iface, err := net.InterfaceByIndex(s.index)
		if err != nil || iface.Name != s.binding.device {
			return ErrAdapterOwnership
		}
		size, err := wireGuardProbeCiphertextSize(len(raw), iface.MTU)
		if err != nil {
			return err
		}
		return reserve(size)
	})
}

func (s *linuxProbeSocket) send(raw []byte, admit func() error) error {
	b := s.binding
	if _, claimed, err := decodeControlPacket(raw, b.local, b.peer); err != nil || !claimed {
		return ErrNoAuthority
	}
	if admit == nil {
		return ErrNoAuthority
	}
	if err := admit(); err != nil {
		return err
	}
	n, err := s.conn.WriteToUDPAddrPort(raw[28:], netip.AddrPortFrom(b.peer, b.port))
	if err != nil {
		return err
	}
	if n != len(raw)-28 {
		return ErrPacketIO
	}
	return nil
}

func (s *linuxProbeSocket) receive() (transport.Control, error) {
	var data [transport.MaxDatagramBytes]byte
	n, _, flags, from, err := s.conn.ReadMsgUDPAddrPort(data[:], nil)
	if err != nil {
		return transport.Control{}, err
	}
	if flags&unix.MSG_TRUNC != 0 || from != netip.AddrPortFrom(s.binding.peer, s.binding.port) {
		return transport.Control{}, ErrPacketIO
	}
	return transport.DecodeControl(data[:n])
}

// Account only authenticated service messages from this candidate. A remote
// sender can add padding; the local interface MTU cannot establish wire size.
func (s *linuxProbeSocket) receiveWireGuard(channel *assignedCipher, broker *kernelCandidateBroker) (transport.Control, error) {
	if s.engine != transport.WireGuard || channel == nil || broker == nil || broker.bind != channel.bind {
		return transport.Control{}, ErrNoAuthority
	}
	value, err := s.receive()
	if err != nil {
		return transport.Control{}, err
	}
	raw, err := controlPacket(s.binding.peer, s.binding.local, value)
	if err != nil {
		return transport.Control{}, err
	}
	err = channel.receiveControl(raw, func(window *probeCycle) error { return broker.chargeIngress(channel, window) })
	if err != nil {
		return transport.Control{}, err
	}
	return value, nil
}

// Only the marked socket may originate UDP/0. Rewrite received packets only
// after matching the authenticated device, signed pair and reserved wire ports.
func (b linuxProbeBinding) rules(table string, group, mark uint32) string {
	const limit = 8 + transport.MaxDatagramBytes
	return fmt.Sprintf("add rule %s output oifgroup %d oifname %q ip saddr %s ip daddr %s meta mark %d udp sport %d udp dport %d udp length 9-%d udp sport set 0 udp dport set 0 accept\n", table, group, b.device, b.local, b.peer, mark, b.port, b.port, limit) +
		fmt.Sprintf("add rule %s input iifgroup %d iifname %q ip saddr %s ip daddr %s udp sport 0 udp dport 0 udp checksum != 0 udp length 9-%d udp sport set %d udp dport set %d accept\n", table, group, b.device, b.peer, b.local, limit, b.port, b.port)
}

// The socket and its deny rules must already be installed. Keep the local
// /32 on the nonpersistent session TUN so losing the session also removes the
// candidate route that specifies that source.
func (l *linuxLink) addProbeRoute(ctx context.Context, b linuxProbeBinding) error {
	if !l.address.IsValid() || !b.local.Is4() || !b.peer.Is4() || b.local == b.peer || !linuxPeerLinkName.MatchString(b.device) {
		return ErrConfiguration
	}
	for _, ip := range []netip.Addr{b.local, b.peer} {
		if err := linuxAddressAvailable(ip); err != nil {
			return err
		}
		routes, err := exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "table", "all", "exact", ip.String()+"/32").Output()
		if err != nil {
			return ErrIPOwnership
		}
		if len(routes) != 0 {
			return ErrIPConflict
		}
	}
	if err := l.ip(ctx, "-4", "address", "add", b.local.String()+"/32", "dev", l.name, "preferred_lft", "0"); err != nil {
		return err
	}
	if l.probes == nil {
		l.probes = make(map[netip.Addr]linuxProbeBinding)
	}
	l.probes[b.peer] = b
	if err := l.ip(ctx, "-4", "route", "add", "table", "main", b.peer.String()+"/32", "dev", b.device, "src", b.local.String(), "proto", "static"); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return errors.Join(err, l.removeProbeRoute(cleanup, b))
	}
	return nil
}

func (l *linuxLink) removeProbeRoute(ctx context.Context, b linuxProbeBinding) error {
	if current, ok := l.probes[b.peer]; !ok || current != b {
		return ErrIPOwnership
	}
	if err := l.ip(ctx, "-4", "address", "del", b.local.String()+"/32", "dev", l.name); err != nil {
		return err
	}
	delete(l.probes, b.peer)
	return nil
}
