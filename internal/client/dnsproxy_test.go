package client

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
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
		NetworkMap: networkMap,
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
}

func TestDNSProxyForwardsPublicNamesToDefaultUpstream(t *testing.T) {
	upstream := startDNSProxyTestUpstream(t, "203.0.113.10")
	response, err := DNSProxyResponse(context.Background(), dnsTestQuery(t, 0x1002, "public.example", dnsTypeA), DNSProxyOptions{
		UpstreamAddr: upstream.addr,
		NetworkMap:   clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
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
		UpstreamAddr: public.addr,
		SplitRules:   []SplitDNSRule{{Domain: "corp.example", Upstream: split.addr}},
		NetworkMap:   clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
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
		UpstreamAddr: public.addr,
		SplitRules:   []SplitDNSRule{{Domain: "corp.example"}},
		NetworkMap:   clientapi.RegisterNodeResponse{Network: clientapi.Network{Name: "default"}},
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

type dnsProxyTestUpstream struct {
	addr    string
	queries chan string
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
