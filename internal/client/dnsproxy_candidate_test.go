package client

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestDNSPairCandidateEscapesContiguousTransportExclusion(t *testing.T) {
	entropy := bytes.NewReader([]byte{0, 0, 0xff, 0xff})
	first := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 49152}}
	selectedTCP := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 65535}}
	selectedUDP := &pairTestUDP{address: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 65535}}
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:0", func(address string) (net.Listener, error) {
		candidate, err := dnsProxyCandidateAddress(address, entropy)
		if err != nil {
			return nil, err
		}
		switch candidate {
		case "127.0.0.1:49152":
			return first, nil
		case "127.0.0.1:65535":
			return selectedTCP, nil
		default:
			t.Fatal("unexpected TCP candidate", candidate)
			return nil, errors.New("unexpected candidate")
		}
	}, func(address string) (net.PacketConn, error) {
		candidate, err := dnsProxyCandidateAddress(address, entropy)
		if err != nil {
			return nil, err
		}
		if candidate == "127.0.0.1:65535" {
			return selectedUDP, nil
		}
		return nil, errors.New("UDP dynamic range excluded below 65000")
	})
	if err != nil || tcp != selectedTCP || udp != selectedUDP || !first.closed || selectedTCP.closed || selectedUDP.closed || entropy.Len() != 0 {
		t.Fatal("pair selection failed to escape exclusion with independent candidates", err)
	}
}

func TestDNSPairCandidatePreservesBindingAndEntropyFailures(t *testing.T) {
	for _, address := range []string{"127.0.0.1:53", "[::1]:5353"} {
		candidate, err := dnsProxyCandidateAddress(address, bytes.NewReader(nil))
		if err != nil || candidate != address {
			t.Fatal("explicit binding changed or consumed entropy", err)
		}
	}
	if _, err := dnsProxyCandidateAddress("[::1]:0", bytes.NewReader([]byte{1})); err == nil {
		t.Fatal("incomplete entropy accepted")
	}
	if candidate, err := dnsProxyCandidateAddress("[::1]:0", bytes.NewReader([]byte{0, 1})); err != nil || candidate != "[::1]:49153" {
		t.Fatal("candidate changed IPv6 binding", err)
	}
}
