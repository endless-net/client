package dataplane

import (
	"context"
	"encoding/binary"
	"io"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	wg "github.com/tailscale/wireguard-go/device"
)

// A private WG device has one source of nonempty IP output, its packet
// injector. Keep one injection outstanding until the corresponding transport
// datagram passes the external send gate. Handshakes and empty keepalives
// can arrive at any time and do not consume this slot.
type linuxPeerOutput struct {
	ctx     context.Context
	stop    context.CancelCauseFunc
	device  *linuxPeerDevice
	serial  sync.Mutex
	pending chan *linuxPeerWrite
}

type linuxPeerWrite struct {
	length int
	check  func(int) error
	done   chan error
}

func newLinuxPeerOutput(ctx context.Context, d *linuxPeerDevice) *linuxPeerOutput {
	ctx, stop := context.WithCancelCause(ctx)
	return &linuxPeerOutput{ctx: ctx, stop: stop, device: d, pending: make(chan *linuxPeerWrite, 1)}
}

func (o *linuxPeerOutput) inject(ip []byte, check func(int) error) error {
	o.serial.Lock()
	defer o.serial.Unlock()
	if err := o.ctx.Err(); err != nil {
		return err
	}
	if len(ip) < 20 || ip[0]>>4 != 4 || int(binary.BigEndian.Uint16(ip[2:4])) != len(ip) {
		return ErrPacketIO
	}
	req := &linuxPeerWrite{length: len(ip), check: check, done: make(chan error, 1)}
	select {
	case o.pending <- req:
	default:
		return ErrEngine
	}
	n, err := o.device.plain.Write(ip)
	if err != nil {
		o.stop(err)
		return err
	}
	if n != len(ip) {
		o.stop(io.ErrShortWrite)
		return io.ErrShortWrite
	}
	select {
	case err := <-req.done:
		return err
	case <-o.ctx.Done():
		return context.Cause(o.ctx)
	}
}

func (o *linuxPeerOutput) forward(send func([]byte, func(int) error) error) (result error) {
	defer func() { o.stop(result) }()
	wake := context.AfterFunc(o.ctx, func() { _ = o.device.wire.SetReadDeadline(time.Now()) })
	defer wake()
	var buf [65535]byte
	for {
		n, from, injected, err := o.device.readKernelOutput(buf[:])
		if err != nil {
			return err
		}
		if from != o.device.endpoint {
			continue
		}
		if err := o.ctx.Err(); err != nil {
			return err
		}
		raw := buf[:n]
		if o.device.outputFilters == nil {
			var err error
			injected, err = classifyWGOutput(raw)
			if err != nil {
				return err
			}
		}
		if injected {
			select {
			case req := <-o.pending:
				if n < req.length+wg.MessageKeepaliveSize {
					return ErrEngine
				}
				req.done <- send(raw, func(size int) error {
					if err := context.Cause(o.ctx); err != nil {
						return err
					}
					if req.check != nil {
						return req.check(size)
					}
					return nil
				})
			default:
				return ErrEngine
			}
			continue
		}
		if err := send(raw, nil); err != nil {
			return err
		}
	}
}

func classifyWGOutput(raw []byte) (bool, error) {
	if len(raw) < 4 {
		return false, ErrEngine
	}
	switch binary.LittleEndian.Uint32(raw) {
	case wg.MessageInitiationType:
		if len(raw) == wg.MessageInitiationSize {
			return false, nil
		}
	case wg.MessageResponseType:
		if len(raw) == wg.MessageResponseSize {
			return false, nil
		}
	case wg.MessageCookieReplyType:
		if len(raw) == wg.MessageCookieReplySize {
			return false, nil
		}
	case wg.MessageTransportType:
		if len(raw) >= wg.MessageKeepaliveSize {
			return len(raw) > wg.MessageKeepaliveSize, nil
		}
	}
	return false, ErrEngine
}

func (d *linuxPeerDevice) readKernelOutput(buf []byte) (int, netip.AddrPort, bool, error) {
	if d.outputFilters == nil {
		n, from, err := d.wire.ReadFromUDPAddrPort(buf)
		return n, from, false, err
	}
	var aux [64]byte
	n, oob, flags, from, err := d.wire.ReadMsgUDPAddrPort(buf, aux[:])
	if err != nil || from != d.endpoint {
		return n, from, false, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return 0, from, false, ErrPacketIO
	}
	msgs, err := unix.ParseSocketControlMessage(aux[:oob])
	if err != nil {
		return 0, from, false, err
	}
	for _, msg := range msgs {
		if msg.Header.Level == unix.IPPROTO_IP && msg.Header.Type == unix.IP_TOS && len(msg.Data) == 1 {
			switch msg.Data[0] {
			case 0x28:
				return n, from, true, nil
			case 0x30:
				return n, from, false, nil
			}
		}
	}
	return 0, from, false, ErrEngine
}
