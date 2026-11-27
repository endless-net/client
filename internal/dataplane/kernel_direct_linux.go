package dataplane

import (
	"context"
	"net"
	"sync"

	"github.com/endless-net/client/api/transport"
)

var platformDirectPreparer kernelPreparer = prepareLinuxDirect

type linuxDirectEngine struct {
	channel *assignedCipher
	port    *packetPort
	device  *linuxPeerDevice
	workers sync.WaitGroup
}

func prepareLinuxDirect(ctx context.Context, cfg directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
	if cfg.assignment.offer.Profile.Engine != transport.WireGuard && cfg.assignment.offer.Profile.Engine != transport.AmneziaWG {
		return nil, errKernelUnavailable
	}
	device, err := openLinuxPeerDevice(ctx, cfg)
	if err != nil {
		return nil, err
	}
	b := channel.bind
	b.handshake = wireGuardHandshakePacket
	if awg := cfg.assignment.offer.Profile.AWG; awg != nil {
		b.handshake = func(raw []byte) bool { return kernelAWGHandshake(awg, raw) }
	}
	rx, _, err := b.Open(0)
	if err != nil {
		device.Close()
		return nil, err
	}
	engine := &linuxDirectEngine{channel: channel, port: port, device: device}
	out := newLinuxPeerOutput(ctx, device)
	// Each terminal worker cancels its owner. Close runs outside the workers
	// and joins them before the supervisor releases its engine reservation.
	start := func(run func()) {
		engine.workers.Go(func() { defer channel.cancel(); run() })
	}
	start(func() { _ = device.receiveCandidate(rx[0]) })
	start(func() {
		_ = out.forward(func(raw []byte, check func(int) error) error {
			b.mu.Lock()
			addr := b.assignment.peerEndpoint
			b.mu.Unlock()
			ep, err := b.ParseEndpoint(addr)
			if err != nil {
				return err
			}
			return b.send([][]byte{raw}, ep, check)
		})
	})
	start(func() {
		for {
			packet, err := port.next()
			if err != nil {
				return
			}
			check := channel.controlSendGuard(packet.data)
			err = out.inject(packet.data, func(size int) error {
				if !port.allowed(packet.control) || packet.guard != nil && !packet.guard() {
					return ErrNoAuthority
				}
				if check != nil {
					return check(size)
				}
				return nil
			})
			clear(packet.data)
			if err != nil && out.ctx.Err() != nil {
				return
			}
		}
	})
	start(func() {
		var buffer [65539]byte
		for {
			n, err := device.plain.Read(buffer[:])
			if err != nil {
				return
			}
			ip, size, err := splitKernelIPv4Frame(buffer[:n])
			if err != nil {
				continue
			}
			if channel.controlReceived(ip, size) == nil {
				_, _ = port.Write([][]byte{ip}, 0)
			}
		}
	})
	return engine, nil
}

func (e *linuxDirectEngine) Queue(raw []byte, control bool) error {
	return e.port.enqueue(raw, control)
}
func (e *linuxDirectEngine) queueChecked(raw []byte, control bool, guard func() bool) error {
	return e.port.enqueueChecked(raw, control, guard)
}
func (e *linuxDirectEngine) BoundPort() (uint16, error) {
	b := e.channel.bind
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.port == 0 {
		return 0, net.ErrClosed
	}
	return b.port, nil
}
func (e *linuxDirectEngine) SetEndpoint(ctx context.Context, addr string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := e.channel.bind.ParseEndpoint(addr)
	return err
}
func (e *linuxDirectEngine) Close() {
	e.channel.cancel()
	e.channel.bind.shutdown()
	_ = e.port.Close()
	e.device.Close()
	e.workers.Wait()
}
