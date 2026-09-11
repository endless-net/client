package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestExplicitDNSResolverAndNameNotFound(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close() }()
	go func() { _ = serveTCP(tcp) }()
	dns, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dns.Close() }()
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, address, err := dns.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
				continue
			}
			question := query.Questions[0]
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true}, Questions: query.Questions}
			if question.Name.String() == "peer.scenario.endlessnet." {
				response.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}}}
			} else {
				response.RCode = dnsmessage.RCodeNameError
			}
			wire, err := response.Pack()
			if err == nil {
				_, _ = dns.WriteTo(wire, address)
			}
		}
	}()
	_, port, err := net.SplitHostPort(tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := probeDNS("tcp", net.JoinHostPort("peer.scenario.endlessnet", port), dns.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := resolve("peer.scenario.endlessnet", dns.LocalAddr().String(), &output); err != nil || output.String() != "127.0.0.1\n" {
		t.Fatal("explicit DNS resolution failed")
	}
	if err := resolve("absent.scenario.endlessnet", dns.LocalAddr().String(), &output); !errors.Is(err, errNameNotFound) {
		t.Fatal("NXDOMAIN was not distinguished from transport failure")
	}
}

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

func TestSessionHandlesLateReplies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial bool
		current bool
		corrupt bool
		want    string
	}{
		{name: "late complete reply", current: true, want: "ready\nblocked\nok\n"},
		{name: "late partial reply", partial: true, current: true, want: "ready\nblocked\nok\n"},
		{name: "old reply cannot prove current reachability", want: "ready\nblocked\nblocked\n"},
		{name: "unknown reply is fatal", current: true, corrupt: true, want: "ready\nblocked\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			done := make(chan error, 1)
			go func() {
				done <- func() error {
					conn, err := listener.Accept()
					if err != nil {
						return err
					}
					defer func() { _ = conn.Close() }()
					if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
						return err
					}
					first := make([]byte, 32)
					if _, err := io.ReadFull(conn, first); err != nil {
						return err
					}
					offset := 0
					if tc.partial {
						offset = 7
						if _, err := conn.Write(first[:offset]); err != nil {
							return err
						}
					}
					// The next request proves the first exchange has timed out.
					second := make([]byte, 32)
					if _, err := io.ReadFull(conn, second); err != nil {
						return err
					}
					if tc.corrupt {
						first[0] ^= 1
					}
					replies := first[offset:]
					if tc.current {
						replies = append(replies, second...)
					}
					_, err = conn.Write(replies)
					return err
				}()
			}()
			var output bytes.Buffer
			err = session("tcp", listener.Addr().String(), strings.NewReader("exchange\nexchange\n"), &output)
			if tc.corrupt {
				if err == nil || errors.Is(err, errUnreachable) {
					t.Fatal("unknown response was accepted as a network outcome")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if output.String() != tc.want {
				t.Fatalf("got %q, want %q", output.String(), tc.want)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
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
