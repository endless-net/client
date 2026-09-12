// Command packetprobe is a CI-only TCP/UDP application peer. It exchanges a
// fresh random payload so an open port alone cannot satisfy the traffic test.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
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
var errNameNotFound = errors.New("DNS name not found")
var errDeadlineSetup = errors.New("application deadline setup failed")

func resolver(server string) *net.Resolver {
	if server == "" {
		return net.DefaultResolver
	}
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: time.Second}
		return d.DialContext(ctx, network, server)
	}}
}

func resolve(name, server string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addresses, err := resolver(server).LookupIP(ctx, "ip4", name)
	if err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) && dnsError.IsNotFound {
			return errNameNotFound
		}
		return errUnreachable
	}
	if len(addresses) != 1 {
		return errors.New("expected one DNS address")
	}
	_, err = fmt.Fprintln(output, addresses[0].String())
	return err
}

func probe(network, address string) error {
	return probeDNS(network, address, "")
}

// IPv6 traffic assertions use literal IPv6 destinations and must never fall
// back to IPv4. Existing hostname/DNS scenarios explicitly exercise IPv4.
func probeNetwork(network, address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() && !ip.Is4In6() {
			return network + "6"
		}
	}
	return network + "4"
}

func probeDNS(network, address, dnsServer string) error {
	if network != "tcp" && network != "udp" {
		return errors.New("network must be tcp or udp")
	}
	dialer := net.Dialer{Timeout: time.Second, Resolver: resolver(dnsServer)}
	conn, err := dialer.Dial(probeNetwork(network, address), address)
	if err != nil {
		return errUnreachable
	}
	defer func() { _ = conn.Close() }()
	x := applicationExchange{datagram: network == "udp"}
	return x.exchange(conn)
}

// Keep known outstanding requests and partial TCP frames across deadlines.
// A delayed echo can be discarded, but only the current nonce proves success.
type applicationExchange struct {
	datagram      bool
	pending       map[[32]byte]struct{}
	completed     map[[32]byte]struct{}
	recent        [128][32]byte
	nextCompleted int
	frame         [32]byte
	filled        int
}

func (x *applicationExchange) exchange(conn net.Conn) error {
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return errDeadlineSetup
	}
	if len(x.pending) >= 128 {
		return errors.New("too many outstanding application requests")
	}
	var request [32]byte
	if _, err := rand.Read(request[:]); err != nil {
		return err
	}
	n, err := conn.Write(request[:])
	if n != 0 && n != len(request) {
		return errors.New("partial application request write")
	}
	if n == len(request) {
		if x.pending == nil {
			x.pending = make(map[[32]byte]struct{})
		}
		x.pending[request] = struct{}{}
	}
	if err != nil || n == 0 {
		return errUnreachable
	}
	for {
		if x.datagram {
			// Preserve datagram boundaries and reject truncated/oversized echoes.
			var packet [33]byte
			n, err := conn.Read(packet[:])
			if err != nil {
				return errUnreachable
			}
			if n != len(x.frame) {
				return errors.New("application response length mismatch")
			}
			copy(x.frame[:], packet[:n])
		} else {
			n, err := io.ReadFull(conn, x.frame[x.filled:])
			x.filled += n
			if err != nil {
				return errUnreachable
			}
		}
		reply := x.frame
		x.filled = 0
		if _, known := x.pending[reply]; !known {
			if x.datagram {
				if _, duplicate := x.completed[reply]; duplicate {
					continue
				}
			}
			different, zero := 0, 0
			for i, value := range reply {
				if value != request[i] {
					different++
				}
				if value == 0 {
					zero++
				}
			}
			return fmt.Errorf("application response mismatch: different_bytes=%d zero_bytes=%d request_sha256=%x reply_sha256=%x", different, zero, sha256.Sum256(request[:]), sha256.Sum256(reply[:]))
		}
		delete(x.pending, reply)
		if x.datagram {
			if x.completed == nil {
				x.completed = make(map[[32]byte]struct{})
			}
			delete(x.completed, x.recent[x.nextCompleted])
			x.recent[x.nextCompleted] = reply
			x.nextCompleted = (x.nextCompleted + 1) % len(x.recent)
			x.completed[reply] = struct{}{}
		}
		if reply == request {
			return nil
		}
	}
}

// session keeps one network connection while the parent changes policy. Its
// stdin/stdout protocol exposes only readiness and an exchange outcome.
func session(network, address string, input io.Reader, output io.Writer) error {
	if network != "tcp" && network != "udp" {
		return errors.New("network must be tcp or udp")
	}
	conn, err := net.DialTimeout(probeNetwork(network, address), address, time.Second)
	if err != nil {
		return errUnreachable
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprintln(output, "ready"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	x := applicationExchange{datagram: network == "udp"}
	for scanner.Scan() {
		if scanner.Text() != "exchange" {
			return errors.New("invalid session command")
		}
		outcome := "ok"
		if err := x.exchange(conn); errors.Is(err, errUnreachable) {
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
	mode := fs.String("mode", "probe", "probe, resolve, session or serve")
	address := fs.String("address", "", "IPv4 address and port")
	network := fs.String("network", "tcp", "tcp or udp for probes")
	dnsServer := fs.String("dns", "", "explicit DNS resolver host:port")
	_ = fs.Parse(os.Args[1:])
	var err error
	switch *mode {
	case "serve":
		err = serve(*address)
	case "probe":
		err = probeDNS(*network, *address, *dnsServer)
	case "resolve":
		err = resolve(*address, *dnsServer, os.Stdout)
	case "session":
		err = session(*network, *address, os.Stdin, os.Stdout)
	default:
		err = errors.New("mode must be serve, probe, resolve or session")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, errNameNotFound) {
			os.Exit(3)
		}
		if errors.Is(err, errUnreachable) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
