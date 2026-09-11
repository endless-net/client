// Command packetprobe is a CI-only TCP/UDP application peer. It exchanges a
// fresh random payload so an open port alone cannot satisfy the traffic test.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

func serveTCP(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer func() { _ = conn.Close() }()
			for {
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				body := make([]byte, 32)
				if _, err := io.ReadFull(conn, body); err != nil {
					return
				}
				if _, err := conn.Write(body); err != nil {
					return
				}
			}
		}()
	}
}

func serveUDP(conn net.PacketConn) error {
	buffer := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			return err
		}
		if n == 32 {
			_, _ = conn.WriteTo(buffer[:n], addr)
		}
	}
}

func serve(address string) error {
	tcp, err := net.Listen("tcp4", address)
	if err != nil {
		return err
	}
	defer func() { _ = tcp.Close() }()
	udp, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		return err
	}
	defer func() { _ = udp.Close() }()
	done := make(chan error, 2)
	go func() { done <- serveTCP(tcp) }()
	go func() { done <- serveUDP(udp) }()
	return <-done
}

var errUnreachable = errors.New("application exchange unavailable")

func probe(network, address string) error {
	if network != "tcp" && network != "udp" {
		return errors.New("network must be tcp or udp")
	}
	conn, err := net.DialTimeout(network+"4", address, time.Second)
	if err != nil {
		return errUnreachable
	}
	defer func() { _ = conn.Close() }()
	return exchange(conn)
}

func exchange(conn net.Conn) error {
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	request := make([]byte, 32)
	if _, err := rand.Read(request); err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return errUnreachable
	}
	reply := make([]byte, len(request))
	if _, err := io.ReadFull(conn, reply); err != nil {
		return errUnreachable
	}
	if !bytes.Equal(request, reply) {
		return errors.New("application response mismatch")
	}
	return nil
}

// session keeps one network connection while the parent changes policy. Its
// stdin/stdout protocol exposes only readiness and an exchange outcome.
func session(network, address string, input io.Reader, output io.Writer) error {
	if network != "tcp" && network != "udp" {
		return errors.New("network must be tcp or udp")
	}
	conn, err := net.DialTimeout(network+"4", address, time.Second)
	if err != nil {
		return errUnreachable
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprintln(output, "ready"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if scanner.Text() != "exchange" {
			return errors.New("invalid session command")
		}
		outcome := "ok"
		if err := exchange(conn); errors.Is(err, errUnreachable) {
			outcome = "blocked"
		} else if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, outcome); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	fs := flag.NewFlagSet("packetprobe", flag.ExitOnError)
	mode := fs.String("mode", "probe", "probe, session or serve")
	address := fs.String("address", "", "IPv4 address and port")
	network := fs.String("network", "tcp", "tcp or udp for probes")
	_ = fs.Parse(os.Args[1:])
	var err error
	switch *mode {
	case "serve":
		err = serve(*address)
	case "probe":
		err = probe(*network, *address)
	case "session":
		err = session(*network, *address, os.Stdin, os.Stdout)
	default:
		err = errors.New("mode must be serve, probe or session")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, errUnreachable) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
