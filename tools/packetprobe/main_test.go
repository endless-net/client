package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSessionKeepsOneConnectionAcrossExchanges(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for range 2 {
			body := make([]byte, 32)
			if _, err := io.ReadFull(conn, body); err != nil {
				return
			}
			if _, err := conn.Write(body); err != nil {
				return
			}
		}
	}()
	var output bytes.Buffer
	if err := session("tcp", listener.Addr().String(), strings.NewReader("exchange\nexchange\nexchange\n"), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "ready\nok\nok\nblocked\n" {
		t.Fatal("session did not preserve its connection or distinguish closure")
	}
}

func TestProbeExchangesApplicationPayload(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close() }()
	udp, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udp.Close() }()
	go func() { _ = serveTCP(tcp) }()
	go func() { _ = serveUDP(udp) }()
	for _, protocol := range []string{"tcp", "udp"} {
		if err := probe(protocol, tcp.Addr().String()); err != nil {
			t.Fatalf("%s exchange: %v", protocol, err)
		}
	}
}

func TestProbeRejectsWrongResponseAndClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	defer func() { _ = listener.Close() }()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		data := make([]byte, 32)
		if _, err := io.ReadFull(conn, data); err == nil {
			data[0] ^= 1
			_, _ = conn.Write(data)
		}
	}()
	if err := probe("tcp", address); err == nil || errors.Is(err, errUnreachable) {
		t.Fatal("corrupt application response was not distinguished from network rejection")
	}
	_ = listener.Close()
	if !errors.Is(probe("tcp", address), errUnreachable) {
		t.Fatal("closed port was accepted")
	}
}
