package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/google/uuid"
	"github.com/mdlayher/genetlink"
)

// A host candidate owns one down WireGuard device until the peer acknowledges
// its advertised socket. Its application route is installed after bilateral commit.
type linuxHostEngine struct {
	session *LinuxSession
	relay   *RelayTransport
	channel *assignedCipher
	port    *packetPort
	initial candidateAssignment
	iface   *net.Interface
	proxy   *net.UDPConn
	guard   *linuxKernelListener
	service *linuxProbeSocket
	workers sync.WaitGroup
}

func (s *LinuxSession) prepareHostCandidate(ctx context.Context, cfg directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
	a := cfg.assignment
	if !linuxHostProbeSupported(a.offer.Profile) || a.plan.LocalProbeIPv4 == "" {
		return prepareLinuxDirect(ctx, cfg, channel, port)
	}
	if !cfg.relay.permitsProbeRoute(a.plan.LocalProbeIPv4, a.plan.PeerProbeIPv4) {
		return nil, errKernelUnavailable
	}
	if a.lease != s.lease {
		return nil, ErrNoAuthority
	}
	conn, err := genetlink.Dial(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	proxy, err := openLinuxBrokerSocket(ctx, s.identity.mark)
	if err != nil {
		return nil, err
	}
	prefix, _ := linuxPeerScope(s.identity.owner)
	suffix, err := uuid.NewRandom()
	if err != nil {
		_ = proxy.Close()
		return nil, err
	}
	iface, err := prepareLinuxPeerLink(ctx, conn, cfg, proxy.LocalAddr().(*net.UDPAddr).AddrPort(), prefix+suffix.String()[:5], s.identity.owner)
	if err != nil {
		_ = proxy.Close()
		return nil, err
	}
	a.mapValue = connectivity.Map{}
	e := &linuxHostEngine{session: s, relay: cfg.relay, channel: channel, port: port, initial: a, iface: iface, proxy: proxy}
	if err = exec.CommandContext(ctx, linuxIP, "link", "set", "dev", iface.Name, "mtu", strconv.Itoa(cfg.mtu)).Run(); err == nil {
		_, _, err = channel.bind.Open(0)
	}
	if err != nil {
		_ = proxy.Close()
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return nil, errors.Join(err, e.deleteDevice(cleanup))
	}
	return e, nil
}

func (e *linuxHostEngine) deleteDevice(ctx context.Context) error {
	if _, err := ownedLinuxPeer(ctx, e.iface, e.session.identity.owner); err != nil {
		return err
	}
	return deleteLinuxPeerIndex(ctx, e.iface.Index)
}
func (e *linuxHostEngine) startProbe(a candidateAssignment) error {
	ctx := e.channel.bind.ctx
	_, changed := a.lease.SourceSignal()
	if e.initial.lease != a.lease || !e.initial.sameChannel(a) || !canFinishPreparation(ctx, e.channel.clock, a, changed) {
		return ErrNoAuthority
	}
	if e.guard != nil {
		return e.channel.bind.checkHostSource(changed)
	}
	conn, err := genetlink.Dial(nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	e.guard, err = startLinuxKernelListener(ctx, conn, e.iface, e.session.identity, e.proxy)
	if err != nil {
		e.channel.cancel()
		return err
	}
	e.service, err = openLinuxProbeSocket(ctx, e.session.identity, e.iface.Name, a)
	if err == nil {
		err = e.session.installPeerProbe(ctx, e.service, e.relay)
	}
	if err != nil {
		e.channel.cancel()
		return err
	}
	driver := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), e.guard.port)
	broker, err := attachKernelBroker(kernelPeerLink{socket: e.proxy, driver: driver}, e.channel.bind, a, changed, linuxKernelHandshake(a.offer.Profile))
	if err != nil {
		e.channel.cancel()
		return err
	}
	start := func(run func()) { e.workers.Go(func() { defer e.channel.cancel(); run() }) }
	start(func() { _ = broker.receive() })
	start(func() { _ = broker.send(nil) })
	start(func() {
		select {
		case <-ctx.Done():
		case <-changed:
		}
	})
	start(func() {
		for {
			p, err := e.port.next()
			if err != nil {
				return
			}
			if p.control && e.port.allowed(true) && (p.guard == nil || p.guard()) {
				_ = e.service.sendControl(p.data, e.channel)
			}
			clear(p.data)
		}
	})
	start(func() {
		for ctx.Err() == nil {
			v, err := e.service.receiveControl(e.channel, broker)
			if err != nil {
				var op *net.OpError
				if errors.As(err, &op) {
					return
				}
				continue
			}
			raw, err := controlPacket(e.service.binding.peer, e.service.binding.local, v)
			if err == nil {
				_, _ = e.port.Write([][]byte{raw}, 0)
			}
		}
	})
	return nil
}
func (b *candidateBind) checkHostSource(changed <-chan struct{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapterSource != changed {
		return ErrNoAuthority
	}
	return b.checkAdapterLocked()
}
func (e *linuxHostEngine) Queue(raw []byte, control bool) error {
	return e.queueChecked(raw, control, nil)
}
func (e *linuxHostEngine) queueChecked(raw []byte, control bool, guard func() bool) error {
	if !control {
		return ErrNoAuthority
	}
	return e.port.enqueueChecked(raw, true, guard)
}
func (e *linuxHostEngine) BoundPort() (uint16, error) {
	return e.channel.bind.preparedPort()
}
func (e *linuxHostEngine) SetEndpoint(ctx context.Context, addr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := e.channel.bind.ParseEndpoint(addr)
	return err
}
func (e *linuxHostEngine) Close() {
	e.channel.cancel()
	e.channel.bind.shutdown()
	_ = e.port.Close()
	if e.service != nil {
		_ = e.service.conn.Close()
	}
	_ = e.proxy.Close()
	e.workers.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.session.retireHostCandidate(ctx, e); err != nil {
		e.session.lease.Revoke()
		e.session.cancel()
	}
}

func (s *LinuxSession) retireHostCandidate(ctx context.Context, e *linuxHostEngine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.link != nil && e.service != nil {
		b := e.service.binding
		if current, ok := s.link.probes[b.peer]; ok && current == b {
			_, ip, err := e.initial.addresses()
			if err != nil {
				return err
			}
			if d, ok := s.link.direct[ip]; ok && d.Index == e.iface.Index {
				if err := s.link.switchPeer(ctx, ip, nil, s.identity.owner); err != nil {
					return err
				}
			}
			if err := s.link.removeProbeRoute(ctx, b); err != nil {
				return err
			}
			if err := s.filters(ctx, s.current, s.policy, false, s.peerFilterMode()); err != nil {
				return err
			}
		}
	}
	if err := e.deleteDevice(ctx); err != nil {
		return err
	}
	if e.guard != nil {
		return e.guard.retire(ctx)
	}
	return nil
}

func (s *LinuxSession) selectHostDirect(route *peerRoute, token *directActivation) (*assignedCipher, error) {
	if token == nil || token.direct == nil {
		return nil, ErrNoAuthority
	}
	e, ok := token.direct.cipher.(*linuxHostEngine)
	if !ok {
		return route.SelectDirect(token)
	}
	if !s.mu.TryLock() {
		return nil, connectivity.ErrSourceChanged
	}
	defer s.mu.Unlock()
	if s.stopped || s.link == nil || e.session != s || e.service == nil {
		return nil, ErrNoAuthority
	}
	b := token.direct.bind
	b.mu.Lock()
	a := b.assignment
	b.mu.Unlock()
	_, ip, err := a.addresses()
	if err != nil {
		return nil, err
	}
	if a.mapValue.Cursor() != s.current.Cursor() || s.link.probes[e.service.binding.peer] != e.service.binding {
		return nil, ErrNoAuthority
	}
	var old *assignedCipher
	err = s.link.commitPeerPath(b.ctx, ip, e.iface, s.identity.owner, func() error {
		var commitErr error
		old, commitErr = route.selectDirect(token, func() error {
			if err := b.checkAdapterLocked(); err != nil {
				return err
			}
			if err := s.filters(b.ctx, s.current, s.policy, false, linuxOnline); err != nil {
				return err
			}
			if err := b.checkAdapterLocked(); err != nil {
				return err
			}
			b.adapterProbeOnly = false
			return nil
		})
		return commitErr
	})
	if err != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(b.ctx), 3*time.Second)
		defer cancel()
		if restore := errors.Join(s.link.verify(ctx), s.filters(ctx, s.current, s.policy, false, s.peerFilterMode())); restore != nil {
			s.lease.Revoke()
			s.cancel()
			return nil, errors.Join(err, restore)
		}
	}
	return old, err
}
