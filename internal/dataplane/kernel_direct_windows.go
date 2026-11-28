package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"golang.org/x/sys/windows"
)

var platformDirectPreparer kernelPreparer = prepareWindowsDirect

// The candidate owns the routed probe interface and both encrypted socket
// directions. Installing an application route requires a separate grant.
type windowsDirectEngine struct {
	relay       *RelayTransport
	channel     *assignedCipher
	port        *packetPort
	device      *windowsPeerDevice
	workers     sync.WaitGroup
	routeMu     sync.Mutex
	retireRoute func()
}

func prepareWindowsDirect(ctx context.Context, cfg directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
	if cfg.assignment.offer.Profile.Engine != transport.WireGuard || cfg.assignment.plan.LocalProbeIPv4 == "" {
		return nil, errKernelUnavailable
	}
	device, err := openWindowsPeerDevice(ctx, cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (directPeerEngine, error) { return nil, errors.Join(err, device.Close()) }
	if _, _, err := channel.bind.Open(0); err != nil {
		return fail(err)
	}
	return &windowsDirectEngine{relay: cfg.relay, channel: channel, port: port, device: device}, nil
}

// BeginProbe serializes this with map updates and Close, after the signed
// offer acknowledges our actual public port and the peer's endpoint. Until
// then the adapter is down and cannot queue driver ciphertext across an ACK.
func (e *windowsDirectEngine) startProbe(a candidateAssignment) error {
	channel, port, device := e.channel, e.port, e.device
	ctx := channel.bind.ctx
	_, changed := a.lease.SourceSignal()
	if !a.sameChannel(device.assignment) || a.lease != device.assignment.lease ||
		!canFinishPreparation(ctx, channel.clock, a, changed) {
		return ErrNoAuthority
	}
	if device.driverPort != 0 {
		if changed != device.sourceChanged {
			return ErrNoAuthority
		}
		return nil
	}
	device.assignment, device.sourceChanged = a, changed
	cfg := directCipherConfig{assignment: a, clock: channel.clock}
	if !e.relay.permitsProbeRoute(a.plan.LocalProbeIPv4, a.plan.PeerProbeIPv4) {
		return ErrNoAuthority
	}
	if err := device.activate(ctx, cfg); err != nil {
		channel.cancel()
		return err
	}
	if err := device.beginRoutedProbe(ctx, cfg); err != nil {
		channel.cancel()
		return err
	}
	if !e.relay.permitsProbeRoute(a.plan.LocalProbeIPv4, a.plan.PeerProbeIPv4) {
		channel.cancel()
		return ErrNoAuthority
	}
	broker, err := device.attachCandidate(channel.bind)
	if err != nil {
		channel.cancel()
		return err
	}
	socket := device.probeSocket
	start := func(run func()) {
		e.workers.Go(func() { defer channel.cancel(); run() })
	}
	start(func() { _ = broker.receive() })
	start(func() { _ = broker.send(nil) })
	start(func() {
		select {
		case <-ctx.Done():
		case <-device.sourceChanged:
		}
	})
	start(func() {
		for {
			packet, err := port.next()
			if err != nil {
				return
			}
			if guard := channel.controlSendGuard(packet.data); guard != nil {
				_ = socket.send(packet.data, func(size int) error {
					if !port.allowed(true) || packet.guard != nil && !packet.guard() {
						return ErrNoAuthority
					}
					return guard(size)
				})
			}
			clear(packet.data)
		}
	})
	start(func() {
		var raw [28 + transport.MaxDatagramBytes]byte
		for ctx.Err() == nil {
			n, err := socket.receive(raw[:])
			if errors.Is(err, windows.WSAETIMEDOUT) || errors.Is(err, windows.WSAEMSGSIZE) || errors.Is(err, ErrNoAuthority) {
				continue
			}
			if err != nil {
				return
			}
			if channel.receiveControl(raw[:n], func(window *probeCycle) error {
				return broker.chargeIngress(channel, window)
			}) == nil {
				_, _ = port.Write([][]byte{raw[:n]}, 0)
			}
		}
	})
	return nil
}

func (e *windowsDirectEngine) Queue(raw []byte, control bool) error {
	return e.queueChecked(raw, control, nil)
}

func (e *windowsDirectEngine) queueChecked(raw []byte, control bool, guard func() bool) error {
	if !control {
		return ErrNoAuthority
	}
	return e.port.enqueueChecked(raw, true, guard)
}

func (e *windowsDirectEngine) BoundPort() (uint16, error) {
	return e.channel.bind.preparedPort()
}

func (e *windowsDirectEngine) SetEndpoint(ctx context.Context, addr string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := e.channel.bind.ParseEndpoint(addr)
	return err
}

func (e *windowsDirectEngine) Close() {
	e.channel.cancel()
	e.channel.bind.shutdown()
	_ = e.port.Close()
	_ = e.device.socket.SetReadDeadline(time.Now())
	e.workers.Wait()
	e.routeMu.Lock()
	defer e.routeMu.Unlock()
	if e.retireRoute != nil {
		e.retireRoute()
		e.retireRoute = nil
	}
	_ = e.device.Close()
}
