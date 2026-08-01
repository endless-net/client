package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	"github.com/tailscale/wireguard-go/conn"
)

func TestMagicBindSharesSocketBetweenSTUNAndWireGuard(t *testing.T) {
	stunServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stunServer.Close() }()
	go func() {
		buf := make([]byte, 2048)
		n, remote, readErr := stunServer.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		response, buildErr := buildSTUNBindingResponseForTest(buf[:n], remote)
		if buildErr == nil {
			_, _ = stunServer.WriteToUDP(response, remote)
		}
	}()

	bind := NewMagicBind()
	receive, port, err := bind.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bind.Close() }()
	if port == 0 || bind.LocalPort() != int(port) || len(receive) == 0 {
		t.Fatalf("open port/receive = %d/%d/%d", port, bind.LocalPort(), len(receive))
	}

	results := bind.CheckSTUN(context.Background(), []clientapi.STUNEndpoint{{
		ID:   "local",
		Addr: stunServer.LocalAddr().String(),
	}}, time.Second)
	if len(results) != 1 || !results[0].Reachable {
		t.Fatalf("STUN results = %#v", results)
	}
	mapped, err := net.ResolveUDPAddr("udp", results[0].MappedAddress)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Port != int(port) {
		t.Fatalf("mapped port = %d, want shared WireGuard port %d", mapped.Port, port)
	}

	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	want := []byte("wireguard datagram")
	if _, err := sender.Write(want); err != nil {
		t.Fatal(err)
	}
	bufs := [][]byte{make([]byte, 2048)}
	sizes := make([]int, 1)
	endpoints := make([]conn.Endpoint, 1)
	type receiveResult struct {
		n   int
		err error
	}
	done := make(chan receiveResult, 1)
	go func() {
		n, err := receive[0](bufs, sizes, endpoints)
		done <- receiveResult{n: n, err: err}
	}()
	select {
	case result := <-done:
		if result.err != nil || result.n != 1 || string(bufs[0][:sizes[0]]) != string(want) {
			t.Fatalf("WireGuard receive = n:%d size:%d payload:%q err:%v", result.n, sizes[0], bufs[0][:sizes[0]], result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WireGuard datagram")
	}
}

func TestMagicBindSendAndClose(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = receiver.Close() }()

	bind := NewMagicBind()
	receive, _, err := bind.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := bind.ParseEndpoint(receiver.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := bind.Send([][]byte{[]byte("xxpayload")}, endpoint, 2); err != nil {
		t.Fatal(err)
	}
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != "payload" {
		t.Fatalf("sent datagram = %q, %v", buf[:n], err)
	}

	if err := bind.Close(); err != nil {
		t.Fatal(err)
	}
	bufs := [][]byte{make([]byte, 64)}
	if _, err := receive[0](bufs, make([]int, 1), make([]conn.Endpoint, 1)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("receive after close error = %v, want net.ErrClosed", err)
	}
}

func TestMagicBindAuthenticatedPathProbeUsesSharedWireGuardSocket(t *testing.T) {
	bindA := NewMagicBind()
	_, portA, err := bindA.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bindA.Close() }()
	bindB := NewMagicBind()
	_, portB, err := bindB.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bindB.Close() }()

	privateA := testWireGuardEngineKey(11)
	publicA := testWireGuardEnginePublicKey(11)
	privateB := testWireGuardEngineKey(12)
	publicB := testWireGuardEnginePublicKey(12)
	if err := bindA.ConfigurePathProbing(privateA, publicA, []clientapi.Peer{{ID: "peer-b", PublicKey: publicB}}); err != nil {
		t.Fatal(err)
	}
	if err := bindB.ConfigurePathProbing(privateB, publicB, []clientapi.Peer{{ID: "peer-a", PublicKey: publicA}}); err != nil {
		t.Fatal(err)
	}

	results := bindA.ProbePeerEndpoints(context.Background(), clientapi.Peer{
		ID:        "peer-b",
		PublicKey: publicB,
		Endpoint:  net.JoinHostPort("127.0.0.1", fmt.Sprint(portB)),
	}, time.Second)
	if len(results) != 1 || !results[0].Reachable || results[0].RTT <= 0 {
		t.Fatalf("path probe results = %#v", results)
	}
	observed, err := net.ResolveUDPAddr("udp", results[0].ObservedEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Port != int(portB) || bindA.LocalPort() != int(portA) {
		t.Fatalf("observed/local path probe ports = %d/%d, want %d/%d", observed.Port, bindA.LocalPort(), portB, portA)
	}
}

func TestMagicBindRejectsMismatchedPathProbeIdentity(t *testing.T) {
	bind := NewMagicBind()
	err := bind.ConfigurePathProbing(
		testWireGuardEngineKey(21),
		testWireGuardEnginePublicKey(22),
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched path probe identity error = %v", err)
	}
}
