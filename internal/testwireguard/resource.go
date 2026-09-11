package testwireguard

import (
	"encoding/binary"
	"net/netip"
	"sync"

	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

// The forwarding hop and the resource's network stack communicate using IP
// packets. Only the test's explicit client/resource pair is routable; no NAT,
// ICMP error generation, Internet gateway or Client runtime is implemented here.
func newResourceLink(resource tun.Device, clientIP, resourceIP netip.Addr) (tun.Device, *trafficCounters, func(), func()) {
	router := tuntest.NewChannelTUN()
	done := make(chan struct{})
	counts := &trafficCounters{}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-done:
				return
			case packet, ok := <-router.Inbound:
				if !ok {
					return
				}
				packet = forwardResourcePacket(packet, clientIP, resourceIP)
				if packet == nil {
					continue
				}
				if _, err := resource.Write([][]byte{packet}, 0); err != nil {
					return
				}
				counts.received.Add(1)
			}
		}
	}()
	go func() {
		defer workers.Done()
		buffers := make([][]byte, resource.BatchSize())
		sizes := make([]int, len(buffers))
		for i := range buffers {
			buffers[i] = make([]byte, 65535)
		}
		for {
			n, err := resource.Read(buffers, sizes, 0)
			if err != nil {
				return
			}
			for i := 0; i < n; i++ {
				packet := forwardResourcePacket(buffers[i][:sizes[i]], resourceIP, clientIP)
				if packet == nil {
					continue
				}
				select {
				case router.Outbound <- packet:
					counts.echoed.Add(1)
				case <-done:
					return
				}
			}
		}
	}()
	return router.TUN(), counts, func() { close(done); _ = resource.Close() }, workers.Wait
}

func forwardResourcePacket(packet []byte, source, destination netip.Addr) []byte {
	if len(packet) < 20 {
		return nil
	}
	var hop, header, length int
	switch packet[0] >> 4 {
	case 4:
		header = int(packet[0]&15) * 4
		length = int(binary.BigEndian.Uint16(packet[2:4]))
		if header < 20 || length < header || length > len(packet) || netip.AddrFrom4([4]byte(packet[12:16])) != source || netip.AddrFrom4([4]byte(packet[16:20])) != destination {
			return nil
		}
		if resourceHeaderChecksum(packet[:header]) != 0 {
			return nil
		}
		hop = 8
	case 6:
		if len(packet) < 40 {
			return nil
		}
		length = 40 + int(binary.BigEndian.Uint16(packet[4:6]))
		if length > len(packet) || netip.AddrFrom16([16]byte(packet[8:24])) != source || netip.AddrFrom16([16]byte(packet[24:40])) != destination {
			return nil
		}
		hop = 7
	default:
		return nil
	}
	if packet[hop] <= 1 {
		return nil
	}
	forwarded := append([]byte(nil), packet[:length]...)
	forwarded[hop]--
	if header != 0 {
		forwarded[10], forwarded[11] = 0, 0
		binary.BigEndian.PutUint16(forwarded[10:12], resourceHeaderChecksum(forwarded[:header]))
	}
	return forwarded
}

func resourceHeaderChecksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
