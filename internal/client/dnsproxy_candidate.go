package client

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"strconv"
)

func listenDNSProxyPair(address string) (net.Listener, net.PacketConn, error) {
	return listenDNSProxyPairWith(address,
		func(addr string) (net.Listener, error) {
			candidate, err := dnsProxyCandidateAddress(addr, rand.Reader)
			if err != nil {
				return nil, err
			}
			return net.Listen("tcp", candidate)
		},
		func(addr string) (net.PacketConn, error) {
			candidate, err := dnsProxyCandidateAddress(addr, rand.Reader)
			if err != nil {
				return nil, err
			}
			return net.ListenPacket("udp", candidate)
		})
}

// Independent candidates avoid sequential OS allocations walking an entire
// transport-specific exclusion range. A candidate is never considered available
// until both binds succeed. The pair allocator retains its bounded retry limit.
func dnsProxyCandidateAddress(address string, entropy io.Reader) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if port != "0" {
		return address, nil
	}
	var sample [2]byte
	if _, err := io.ReadFull(entropy, sample[:]); err != nil {
		return "", err
	}
	// The dynamic/private range has 2^14 ports, so masking is unbiased.
	selected := 49152 + int(binary.BigEndian.Uint16(sample[:])&0x3fff)
	return net.JoinHostPort(host, strconv.Itoa(selected)), nil
}
