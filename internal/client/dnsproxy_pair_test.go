package client

import (
	"errors"
	"net"
	"testing"
)

type pairTestTCP struct {
	net.Listener
	address *net.TCPAddr
	closed  bool
}

func (s *pairTestTCP) Addr() net.Addr { return s.address }
func (s *pairTestTCP) Close() error   { s.closed = true; return nil }

type pairTestUDP struct {
	net.PacketConn
	address *net.UDPAddr
	closed  bool
}

func (s *pairTestUDP) LocalAddr() net.Addr { return s.address }
func (s *pairTestUDP) Close() error        { s.closed = true; return nil }

func TestDNSProxyPairDoesNotRecycleRejectedReservation(t *testing.T) {
	denied := errors.New("transport-specific exclusion")
	first := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41001}}
	rejectedUDP := &pairTestUDP{address: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42001}}
	selectedTCP := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41002}}
	selectedUDP := &pairTestUDP{address: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41002}}
	tcpCalls := 0
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:0", func(address string) (net.Listener, error) {
		if address != "127.0.0.1:0" {
			return nil, denied
		}
		tcpCalls++
		// Model an allocator preferring the released excluded port.
		if tcpCalls == 1 || first.closed {
			first.closed = false
			return first, nil
		}
		return selectedTCP, nil
	}, func(address string) (net.PacketConn, error) {
		switch address {
		case "127.0.0.1:0":
			return rejectedUDP, nil
		case "127.0.0.1:41002":
			return selectedUDP, nil
		default:
			return nil, denied
		}
	})
	if err != nil || tcp != selectedTCP || udp != selectedUDP || tcpCalls != 2 {
		t.Fatal("pair selection recycled a rejected ephemeral reservation")
	}
	if !first.closed || !rejectedUDP.closed || selectedTCP.closed || selectedUDP.closed {
		t.Fatal("selection leaked rejected reservations or closed the selected pair")
	}
	_ = tcp.Close()
	_ = udp.Close()
}

func TestDNSProxyPairExhaustionClosesEveryReservation(t *testing.T) {
	denied := errors.New("no common transport port")
	var tcpReservations []*pairTestTCP
	var udpReservations []*pairTestUDP
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:0", func(address string) (net.Listener, error) {
		if address != "127.0.0.1:0" {
			return nil, denied
		}
		socket := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41000 + len(tcpReservations)}}
		tcpReservations = append(tcpReservations, socket)
		return socket, nil
	}, func(address string) (net.PacketConn, error) {
		if address != "127.0.0.1:0" {
			return nil, denied
		}
		socket := &pairTestUDP{address: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42000 + len(udpReservations)}}
		udpReservations = append(udpReservations, socket)
		return socket, nil
	})
	if tcp != nil || udp != nil || !errors.Is(err, denied) || len(tcpReservations) != 8 || len(udpReservations) != 8 {
		t.Fatal("ephemeral selection did not stop at its bounded attempt limit")
	}
	for _, socket := range tcpReservations {
		if !socket.closed {
			t.Error("TCP reservation leaked")
		}
	}
	for _, socket := range udpReservations {
		if !socket.closed {
			t.Error("UDP reservation leaked")
		}
	}
}

func TestDNSProxyPairAllocationFailureClosesPriorReservation(t *testing.T) {
	reservation := &pairTestTCP{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41001}}
	denied := errors.New("paired port excluded")
	allocationFailed := errors.New("ephemeral allocation failed")
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:0",
		func(string) (net.Listener, error) { return reservation, nil },
		func(address string) (net.PacketConn, error) {
			if address == "127.0.0.1:0" {
				return nil, allocationFailed
			}
			return nil, denied
		})
	if tcp != nil || udp != nil || !errors.Is(err, allocationFailed) || !reservation.closed {
		t.Fatal("early allocation failure leaked the previous reservation or lost its error")
	}
}
