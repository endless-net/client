package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestDNSProxyAnswersPeerFromSignedMap(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "default"},
		Peers: []clientapi.Peer{{
			ID:         "node-b",
			Hostname:   "node-b",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1001, "node-b.default.endlessnet", dnsTypeA), DNSProxyOptions{
		NetworkMap:   networkMap,
		ServePeerDNS: true,
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rcode := dnsTestRCode(response); rcode != dnsRCodeNoErr {
		t.Fatalf("rcode = %d", rcode)
	}
	if got := dnsTestAAnswer(t, response); got != "100.64.0.3" {
		t.Fatalf("A answer = %s, want 100.64.0.3", got)
	}
	response, err = DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1002, "node-b.default.endlessnet", dnsTypeAAAA), DNSProxyOptions{
		NetworkMap:   networkMap,
		ServePeerDNS: true,
	}, time.Second)
	if err != nil || dnsTestRCode(response) != dnsRCodeNoErr || binary.BigEndian.Uint16(response[6:8]) != 0 {
		t.Fatal("existing IPv4-only peer name did not return empty AAAA success", err)
	}
	response, err = DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1003, "absent.default.endlessnet", dnsTypeAAAA), DNSProxyOptions{
		NetworkMap:   networkMap,
		ServePeerDNS: true,
	}, time.Second)
	if err != nil || dnsTestRCode(response) != dnsRCodeNX {
		t.Fatal("absent peer name did not return name error", err)
	}
}

func TestDNSProxyDoesNotServePeerNamesWhenMagicDNSIsDisabled(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "default"},
		Peers: []clientapi.Peer{{
			ID: "node-b", Hostname: "node-b", AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1007, "node-b.default.endlessnet", dnsTypeA), DNSProxyOptions{
		NetworkMap: networkMap,
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rcode := dnsTestRCode(response); rcode != dnsRCodeFail {
		t.Fatalf("rcode = %d, want SERVFAIL", rcode)
	}
}

func TestDNSProxyForwardsPublicNamesToDefaultUpstream(t *testing.T) {
	upstream := startDNSProxyTestUpstream(t, "203.0.113.10")
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1002, "public.example", dnsTypeA), DNSProxyOptions{
		UpstreamAddrs: []string{upstream.addr},
		NetworkMap:    clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-upstream.queries; got != "public.example" {
		t.Fatalf("upstream query = %s", got)
	}
	if got := dnsTestAAnswer(t, response); got != "203.0.113.10" {
		t.Fatalf("A answer = %s, want 203.0.113.10", got)
	}
}

func TestDNSProxyForwardsSplitNamesOnlyToSplitUpstream(t *testing.T) {
	public := startDNSProxyTestUpstream(t, "203.0.113.10")
	split := startDNSProxyTestUpstream(t, "10.10.0.53")
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1003, "db.corp.example", dnsTypeA), DNSProxyOptions{
		UpstreamAddrs: []string{public.addr},
		SplitRules:    []SplitDNSRule{{Domain: "corp.example", Upstreams: []string{split.addr}}},
		NetworkMap:    clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-split.queries; got != "db.corp.example" {
		t.Fatalf("split query = %s", got)
	}
	select {
	case got := <-public.queries:
		t.Fatalf("split query leaked to public upstream as %s", got)
	case <-time.After(100 * time.Millisecond):
	}
	if got := dnsTestAAnswer(t, response); got != "10.10.0.53" {
		t.Fatalf("A answer = %s, want 10.10.0.53", got)
	}
}

func TestDNSProxySplitDomainWithoutUpstreamFailsClosed(t *testing.T) {
	public := startDNSProxyTestUpstream(t, "203.0.113.10")
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1004, "db.corp.example", dnsTypeA), DNSProxyOptions{
		UpstreamAddrs: []string{public.addr},
		SplitRules:    []SplitDNSRule{{Domain: "corp.example"}},
		NetworkMap:    clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rcode := dnsTestRCode(response); rcode != dnsRCodeFail {
		t.Fatalf("rcode = %d, want SERVFAIL", rcode)
	}
	select {
	case got := <-public.queries:
		t.Fatalf("fail-closed split query leaked to public upstream as %s", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDNSProxyFailsOverToTheNextGlobalUpstream(t *testing.T) {
	upstream := startDNSProxyTestUpstream(t, "203.0.113.20")
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1006, "public.example", dnsTypeA), DNSProxyOptions{
		UpstreamAddrs: []string{"127.0.0.1:1", upstream.addr},
		NetworkMap:    clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got := dnsTestAAnswer(t, response); got != "203.0.113.20" {
		t.Fatalf("A answer = %s, want failover response", got)
	}
}

func TestDNSProxyServesTCPOnTheUDPAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "default"},
		Peers: []clientapi.Peer{{
			ID: "node-b", Hostname: "node-b", AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	go func() {
		done <- ServeDNSProxy(ctx, DNSProxyOptions{
			ListenAddr: "127.0.0.1:0", NetworkMap: networkMap,
			ServePeerDNS: true,
			Ready:        func(addr string) { ready <- addr },
		})
	}()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("DNS proxy exited before readiness: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("DNS proxy did not become ready")
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	query := dnsTestQuery(t, 0x1005, "node-b.default.endlessnet", dnsTypeA)
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
	framed = append(framed, query...)
	if _, err := conn.Write(framed); err != nil {
		t.Fatal(err)
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, int(binary.BigEndian.Uint16(size[:])))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got := dnsTestAAnswer(t, response); got != "100.64.0.3" {
		t.Fatalf("TCP A answer = %s", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DNS proxy did not stop after cancellation")
	}
}

type dnsProxyTestUpstream struct {
	addr    string
	queries chan string
}

func TestDNSProxyPairRecoversFromTransportSpecificExclusion(t *testing.T) {
	denied := errors.New("simulated UDP port exclusion")
	var first net.Listener
	var udpAddresses []string
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:0",
		func(addr string) (net.Listener, error) {
			listener, err := net.Listen("tcp4", addr)
			if err == nil {
				t.Cleanup(func() { _ = listener.Close() })
			}
			if first == nil {
				first = listener
			}
			return listener, err
		},
		func(addr string) (net.PacketConn, error) {
			udpAddresses = append(udpAddresses, addr)
			if len(udpAddresses) == 1 {
				return nil, denied
			}
			return net.ListenPacket("udp4", addr)
		})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close(); _ = udp.Close() }()
	if len(udpAddresses) < 2 || udpAddresses[1] != "127.0.0.1:0" {
		t.Fatal("UDP did not choose the replacement ephemeral port")
	}
	if tcp.Addr().String() != udp.LocalAddr().String() {
		t.Fatal("DNS transports do not share an address")
	}
	_ = first.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
	if conn, err := first.Accept(); !errors.Is(err, net.ErrClosed) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("failed TCP reservation was not closed")
	}
}

func TestDNSProxyPairDoesNotMoveExplicitPort(t *testing.T) {
	denied := errors.New("simulated UDP port exclusion")
	var first net.Listener
	calls := 0
	tcp, udp, err := listenDNSProxyPairWith("127.0.0.1:5353",
		func(addr string) (net.Listener, error) {
			if addr != "127.0.0.1:5353" {
				t.Fatal("explicit port changed")
			}
			var err error
			// Allocate a test reservation without requiring host port 5353.
			first, err = net.Listen("tcp4", "127.0.0.1:0")
			if err == nil {
				t.Cleanup(func() { _ = first.Close() })
			}
			return first, err
		},
		func(string) (net.PacketConn, error) { calls++; return nil, denied })
	if !errors.Is(err, denied) || tcp != nil || udp != nil || calls != 1 {
		t.Fatal("explicit bind failure was retried or ignored")
	}
	_ = first.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
	if conn, err := first.Accept(); !errors.Is(err, net.ErrClosed) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("failed TCP reservation was not closed")
	}
}

func startDNSProxyTestUpstream(t *testing.T, answer string) dnsProxyTestUpstream {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	queries := make(chan string, 4)
	go func() {
		buf := make([]byte, dnsMaxUDPBytes)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			request := append([]byte(nil), buf[:n]...)
			question, err := parseDNSQuestion(request)
			if err != nil {
				continue
			}
			queries <- question.Name
			response := dnsTestResponseA(request, question, answer)
			_, _ = conn.WriteTo(response, addr)
		}
	}()
	return dnsProxyTestUpstream{addr: conn.LocalAddr().String(), queries: queries}
}

func dnsTestQuery(t *testing.T, id uint16, name string, qtype uint16) []byte {
	t.Helper()
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[0:2], id)
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	for _, label := range stringsSplitDNSName(name) {
		packet = append(packet, byte(len(label)))
		packet = append(packet, []byte(label)...)
	}
	packet = append(packet, 0)
	packet = binary.BigEndian.AppendUint16(packet, qtype)
	packet = binary.BigEndian.AppendUint16(packet, dnsClassIN)
	return packet
}

func dnsTestResponseA(request []byte, question dnsQuestion, answer string) []byte {
	addr := netip.MustParseAddr(answer).As4()
	payload := []byte{0xc0, 0x0c}
	payload = binary.BigEndian.AppendUint16(payload, question.Type)
	payload = binary.BigEndian.AppendUint16(payload, dnsClassIN)
	payload = binary.BigEndian.AppendUint32(payload, 30)
	payload = binary.BigEndian.AppendUint16(payload, 4)
	payload = append(payload, addr[:]...)
	return dnsResponseHeader(request, dnsRCodeNoErr, 1, payload)
}

func dnsTestRCode(packet []byte) uint16 {
	if len(packet) < 4 {
		return 0xffff
	}
	return binary.BigEndian.Uint16(packet[2:4]) & 0x000f
}

func dnsTestAAnswer(t *testing.T, packet []byte) string {
	t.Helper()
	question, err := parseDNSQuestion(packet)
	if err != nil {
		t.Fatal(err)
	}
	offset := question.QuestionEnd
	if offset+16 > len(packet) {
		t.Fatalf("DNS response too short for answer: %x", packet)
	}
	rdlength := int(binary.BigEndian.Uint16(packet[offset+10 : offset+12]))
	if rdlength != 4 || offset+12+rdlength > len(packet) {
		t.Fatalf("unexpected A rdlength in %x", packet[offset:])
	}
	var raw [4]byte
	copy(raw[:], packet[offset+12:offset+16])
	addr := netip.AddrFrom4(raw)
	return addr.String()
}

func stringsSplitDNSName(name string) []string {
	name = normalizeDNSName(name)
	if name == "" {
		return nil
	}
	var labels []string
	for _, label := range strings.Split(name, ".") {
		if label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}
