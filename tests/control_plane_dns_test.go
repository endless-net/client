package tests

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testclient"
	ipc "github.com/endless-net/client/ipc/v2"
	"golang.org/x/net/dns/dnsmessage"
)

// HC-025/HC-026: real CLI DNS proxy, published DNS wire messages and signed
// map changes on every platform. Each proxy invocation loads a fresh map;
// this does not claim live reload or system resolver configuration.
func TestControlPlaneDNSWireRecovery(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			for _, host := range []string{"127.0.0.1", "::1"} {
				t.Run(host, func(t *testing.T) {
					t.Run("complete-udp", func(t *testing.T) { exerciseDNSWireRecovery(t, network, host, false) })
					t.Run("truncated-udp", func(t *testing.T) { exerciseDNSWireRecovery(t, network, host, true) })
				})
			}
		})
	}
}

func exerciseDNSWireRecovery(t *testing.T, upstreamNetwork, listenHost string, truncated bool) {
	t.Helper()
	s, n, id := controlScenario(t)
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.Stop()
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	peer := api.Peer{ID: "dns-wire-peer", Hostname: "wire-peer", PublicKey: public, AllowedIPs: []string{"100.90.0.20/32", "fd7a:115c:a1e0::20/128"}}
	globalAddress, globalQueries, _ := dnsContractUpstream(t, upstreamNetwork, truncated, [4]byte{203, 0, 113, 4})
	splitAddress, splitQueries, setSplitCode := dnsContractUpstream(t, upstreamNetwork, truncated, [4]byte{198, 51, 100, 7})
	for _, present := range []bool{true, false, true} {
		var peers []api.Peer
		if present {
			peers = []api.Peer{peer}
		}
		if err := s.UpdatePeers(id, peers); err != nil {
			t.Fatal(err)
		}
		n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
		address, stop := startClientDNS(t, n, listenHost, "--upstream", globalAddress, "--split", "corp.test="+splitAddress, "--split", "blocked.corp.test=")
		for _, transport := range []string{"udp", "tcp"} {
			peerCode, peerAddress := dnsmessage.RCodeNameError, ""
			if present {
				peerCode, peerAddress = dnsmessage.RCodeSuccess, "100.90.0.20"
			}
			assertDNSWire(t, transport, address, "wire-peer.scenario.endlessnet.", peerCode, peerAddress)
			peerIPv6 := ""
			if present {
				peerIPv6 = "fd7a:115c:a1e0::20"
			}
			assertDNSWireType(t, transport, address, "wire-peer.scenario.endlessnet.", dnsmessage.TypeAAAA, peerCode, peerIPv6)
			assertDNSWireType(t, transport, address, "absent.scenario.endlessnet.", dnsmessage.TypeAAAA, dnsmessage.RCodeNameError, "")
			assertDNSWire(t, transport, address, "absent.scenario.endlessnet.", dnsmessage.RCodeNameError, "")
			assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
			assertDNSWire(t, transport, address, "db.corp.test.", dnsmessage.RCodeSuccess, "198.51.100.7")
			assertDNSWire(t, transport, address, "db.blocked.corp.test.", dnsmessage.RCodeServerFailure, "")
			// A failing selected resolver must not leak the private query to the
			// healthy global resolver. Recovery uses this same proxy process.
			setSplitCode(dnsmessage.RCodeServerFailure)
			assertDNSWire(t, transport, address, "db.corp.test.", dnsmessage.RCodeServerFailure, "")
			assertDNSWire(t, transport, address, "public.example.", dnsmessage.RCodeSuccess, "203.0.113.4")
			assertDNSWire(t, transport, address, "wire-peer.scenario.endlessnet.", peerCode, peerAddress)
			setSplitCode(dnsmessage.RCodeSuccess)
			assertDNSWire(t, transport, address, "db.corp.test.", dnsmessage.RCodeSuccess, "198.51.100.7")
		}
		stop()
	}
	// A successful answer alone cannot prove split-DNS isolation. Each fixture
	// records the actual wire questions and must receive only its own domain.
	for _, observation := range []struct {
		queries []string
		want    string
		count   int
	}{
		{globalQueries(), "public.example.", 12}, {splitQueries(), "db.corp.test.", 18},
	} {
		count := observation.count
		if truncated {
			count *= 2 // Every question must reach both UDP and TCP.
		}
		if len(observation.queries) != count {
			t.Fatal("unexpected DNS upstream query count")
		}
		for _, query := range observation.queries {
			if query != observation.want {
				t.Fatal("private or split DNS query leaked to another upstream")
			}
		}
	}
}

func startClientDNS(t *testing.T, n *testclient.Node, listenHost string, options ...string) (string, func()) {
	t.Helper()
	args := []string{"dns", "serve", "--config", n.Config, "--listen", net.JoinHostPort(listenHost, "0"), "--domain", "scenario.endlessnet", "--timeout", "1s"}
	cmd := exec.Command(n.Binary, append(args, options...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- strings.TrimPrefix(scanner.Text(), "dns proxy listening on ")
		} else {
			ready <- ""
		}
	}()
	select {
	case address := <-ready:
		parsed, err := netip.ParseAddrPort(address)
		if err != nil || parsed.Addr() != netip.MustParseAddr(listenHost) || parsed.Port() == 0 {
			t.Fatal("DNS CLI did not report a loopback listener")
		}
		return address, stop
	case <-time.After(5 * time.Second):
		t.Fatal("DNS CLI startup deadline")
		return "", stop
	}
}

func assertDNSWire(t *testing.T, transport, address, name string, code dnsmessage.RCode, expected string) {
	t.Helper()
	assertDNSWireType(t, transport, address, name, dnsmessage.TypeA, code, expected)
}

func assertDNSWireType(t *testing.T, transport, address, name string, family dnsmessage.Type, code dnsmessage.RCode, expected string) {
	t.Helper()
	qname, err := dnsmessage.NewName(name)
	if err != nil {
		t.Fatal(err)
	}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 0x6142, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: qname, Type: family, Class: dnsmessage.ClassINET}}}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout(transport, address, time.Second)
	if err != nil {
		t.Fatal("DNS listener connection failed")
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if transport == "tcp" {
		wire = append(binary.BigEndian.AppendUint16(nil, uint16(len(wire))), wire...)
	}
	if _, err := conn.Write(wire); err != nil {
		t.Fatal("DNS query send failed")
	}
	var reply []byte
	if transport == "tcp" {
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			t.Fatal("DNS TCP response header unavailable")
		}
		reply = make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatal("DNS TCP response incomplete")
		}
	} else {
		reply = make([]byte, 4096)
		n, err := conn.Read(reply)
		if err != nil {
			t.Fatal("DNS UDP response unavailable")
		}
		reply = reply[:n]
	}
	var response dnsmessage.Message
	if err := response.Unpack(reply); err != nil {
		t.Fatal("invalid DNS response")
	}
	if !response.Response || response.ID != query.ID || response.RCode != code || len(response.Questions) != 1 || response.Questions[0] != query.Questions[0] {
		t.Fatalf("unexpected %s DNS response for %s: code=%s", transport, name, response.RCode)
	}
	if expected == "" {
		if len(response.Answers) != 0 {
			t.Fatal("denied DNS response contained an answer")
		}
		return
	}
	if len(response.Answers) != 1 {
		t.Fatal("DNS did not return exactly one address")
	}
	answer := response.Answers[0]
	var actual netip.Addr
	switch body := answer.Body.(type) {
	case *dnsmessage.AResource:
		actual = netip.AddrFrom4(body.A)
	case *dnsmessage.AAAAResource:
		actual = netip.AddrFrom16(body.AAAA)
	}
	if answer.Header.Name != qname || answer.Header.Type != family || answer.Header.Class != dnsmessage.ClassINET || !actual.IsValid() || actual.String() != expected {
		t.Fatal("DNS address does not match the published map or selected upstream")
	}
}

func dnsContractUpstream(t *testing.T, network string, truncated bool, address [4]byte) (string, func() []string, func(dnsmessage.RCode)) {
	t.Helper()
	host := "127.0.0.1"
	if network == "udp6" {
		host = "::1"
	}
	endpoint := net.JoinHostPort(host, "0")
	var listener net.Listener
	if truncated {
		// Let TCP allocate its own port, respecting existing TCP connections and
		// TIME_WAIT. A UDP-allocated port need not be available to TCP on Windows.
		var err error
		listener, err = net.Listen(strings.Replace(network, "udp", "tcp", 1), endpoint)
		if err != nil {
			t.Fatal("could not allocate DNS TCP fallback fixture")
		}
		t.Cleanup(func() { _ = listener.Close() })
		endpoint = listener.Addr().String()
	}
	conn, err := net.ListenPacket(network, endpoint)
	if err != nil {
		t.Fatal("could not bind DNS UDP fixture at the selected endpoint")
	}
	t.Cleanup(func() { _ = conn.Close() })
	var mu sync.Mutex
	var queries []string
	code := dnsmessage.RCodeSuccess
	answer := func(raw []byte, tcp bool) []byte {
		var query dnsmessage.Message
		if query.Unpack(raw) != nil || len(query.Questions) != 1 {
			return nil
		}
		q := query.Questions[0]
		mu.Lock()
		queries = append(queries, q.Name.String())
		responseCode := code
		mu.Unlock()
		response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RCode: responseCode}, Questions: query.Questions,
			Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: address}}}}
		if responseCode != dnsmessage.RCodeSuccess {
			response.Answers = nil
		}
		if truncated && !tcp {
			response.Truncated, response.Answers = true, nil
		}
		wire, err := response.Pack()
		if err != nil {
			return nil
		}
		return wire
	}
	if truncated {
		go func() {
			for {
				stream, err := listener.Accept()
				if err != nil {
					return
				}
				func() {
					defer func() { _ = stream.Close() }()
					_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
					var size [2]byte
					if _, err := io.ReadFull(stream, size[:]); err != nil {
						return
					}
					raw := make([]byte, binary.BigEndian.Uint16(size[:]))
					if _, err := io.ReadFull(stream, raw); err != nil {
						return
					}
					wire := answer(raw, true)
					binary.BigEndian.PutUint16(size[:], uint16(len(wire)))
					_, _ = io.Copy(stream, bytes.NewReader(append(size[:], wire...)))
				}()
			}
		}()
	}
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, remote, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			if wire := answer(buffer[:n], false); wire != nil {
				_, _ = conn.WriteTo(wire, remote)
			}
		}
	}()
	return conn.LocalAddr().String(), func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), queries...) }, func(value dnsmessage.RCode) { mu.Lock(); defer mu.Unlock(); code = value }
}
