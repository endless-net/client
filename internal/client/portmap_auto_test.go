package client

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseRouteGateway(t *testing.T) {
	tests := []struct {
		goos   string
		output string
		want   string
	}{
		{goos: "windows", output: "192.168.1.1\r\n", want: "192.168.1.1"},
		{goos: "linux", output: "45.152.87.196 via 192.168.1.1 dev eth0 src 192.168.1.20", want: "192.168.1.1"},
		{goos: "darwin", output: "   route to: 45.152.87.196\ngateway: 192.168.1.1\ninterface: en0", want: "192.168.1.1"},
		{goos: "linux", output: "45.152.87.196 dev wg0 src 10.8.1.1", want: ""},
	}
	for _, test := range tests {
		if got := parseRouteGateway(test.goos, test.output); got != test.want {
			t.Fatalf("parseRouteGateway(%q, %q) = %q, want %q", test.goos, test.output, got, test.want)
		}
	}
}

func TestPublicSTUNDestinationsSelectsPublicIPv4(t *testing.T) {
	got := publicSTUNDestinations(context.Background(), []STUNCheckResult{
		{Addr: "127.0.0.1:3478"},
		{Addr: "45.152.87.196:3478"},
		{Addr: "[2001:db8::1]:3478"},
		{Addr: "45.152.87.196:3479"},
	})
	if len(got) != 1 || got[0].String() != "45.152.87.196" {
		t.Fatalf("public STUN destinations = %#v", got)
	}
}

func TestMapPCPSendsClientAddressAndReusableNonce(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	nonces := make(chan []byte, 2)
	serverErr := make(chan error, 1)
	go func() {
		for index := 0; index < 2; index++ {
			request := make([]byte, 128)
			n, remote, readErr := server.ReadFromUDP(request)
			if readErr != nil {
				serverErr <- readErr
				return
			}
			if n != 60 || request[0] != 2 || request[1] != 1 || request[36] != 17 {
				serverErr <- &testPortMappingError{"invalid PCP request"}
				return
			}
			if !net.IP(request[8:24]).Equal(net.IPv4(127, 0, 0, 1)) {
				serverErr <- &testPortMappingError{"PCP request missing local client address"}
				return
			}
			nonce := append([]byte(nil), request[24:36]...)
			nonces <- nonce
			response := make([]byte, 60)
			response[0] = 2
			response[1] = 0x81
			binary.BigEndian.PutUint32(response[4:8], 120)
			copy(response[24:36], nonce)
			response[36] = 17
			copy(response[40:44], request[40:44])
			copy(response[44:60], net.ParseIP("203.0.113.10").To16())
			if _, writeErr := server.WriteToUDP(response, remote); writeErr != nil {
				serverErr <- writeErr
				return
			}
		}
		serverErr <- nil
	}()

	request := PortMappingRequest{
		Gateway:      server.LocalAddr().String(),
		InternalPort: 51820,
		ExternalPort: 51820,
		Lifetime:     2 * time.Minute,
		Timeout:      time.Second,
	}
	first := MapPCP(context.Background(), request)
	if !first.OK || first.MappedEndpoint != "203.0.113.10:51820" || len(first.pcpNonce) != 12 {
		t.Fatalf("first PCP mapping = %#v", first)
	}
	request.pcpNonce = first.pcpNonce
	second := MapPCP(context.Background(), request)
	if !second.OK || string(second.pcpNonce) != string(first.pcpNonce) {
		t.Fatalf("renewed PCP mapping = %#v", second)
	}
	firstNonce := <-nonces
	secondNonce := <-nonces
	if strings.Trim(string(firstNonce), "\x00") == "" || string(firstNonce) != string(secondNonce) {
		t.Fatalf("PCP nonces = %x / %x", firstNonce, secondNonce)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

type testPortMappingError struct{ message string }

func (e *testPortMappingError) Error() string { return e.message }
