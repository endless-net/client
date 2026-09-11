package client

import (
	"context"
	"errors"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestWireGuardInspectionDuringBlockedInterfaceCreation(t *testing.T) {
	entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	}
	creationFailure := errors.New("injected interface creation failure")
	calls := 0
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		router:    &testWireGuardEngineRouter{},
		tunFactory: func(string, int) (tun.Device, error) {
			calls++
			if calls == 1 {
				close(entered)
				<-unblock
				return nil, creationFailure
			}
			return tuntest.NewChannelTUN().TUN(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	config := Config{PrivateKey: testWireGuardEngineKey(1)}
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24"},
		Node:    clientapi.Node{ID: "node-1", Hostname: "node-1", NetworkID: "net-1", PublicKey: testWireGuardEnginePublicKey(1), AssignedIP: "100.64.0.2"},
	}
	var configureErr error
	go func() {
		_, configureErr = engine.Configure(context.Background(), config, networkMap)
		close(done)
	}()
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("blocked configuration did not finish after release")
		}
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("configuration did not reach interface creation")
	}
	available := make(chan bool, 1)
	go func() {
		_, ok := engine.TryInspection()
		available <- ok
	}()
	select {
	case ok := <-available:
		if ok {
			t.Fatal("inspection claimed availability during blocked interface creation")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("inspection waited for blocked platform interface creation")
	}
	release()
	<-done
	if !errors.Is(configureErr, creationFailure) {
		t.Fatal("configuration lost the injected interface error")
	}
	if _, err := engine.Configure(context.Background(), config, networkMap); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		inspection, ok := engine.TryInspection()
		if ok {
			if !inspection.OK || inspection.Interface == "" || inspection.ListenPort == 0 {
				t.Fatalf("recovered inspection is incomplete: ok=%t interface_present=%t listening=%t", inspection.OK, inspection.Interface != "", inspection.ListenPort != 0)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("inspection remained unavailable after successful interface creation")
		}
		time.Sleep(time.Millisecond)
	}
}
