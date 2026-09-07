package client

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	relay "github.com/endless-net/relay/protocol/v1"
)

func TestRelayBridgeRejectsSenderNetworkSubstitution(t *testing.T) {
	wg, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wg.Close() }()
	endpoint, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = endpoint.Close() }()
	input, sender := net.Pipe()
	defer func() { _ = input.Close(); _ = sender.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errors := make(chan error, 1)
	binding := relayDataplaneBinding{peer: clientapi.Peer{ID: "peer", NetworkID: "shared-network"}, conn: endpoint}
	go relayDataplaneForwardRelayToUDP(ctx, input, bindingsByPeerID([]relayDataplaneBinding{binding}), wg.LocalAddr().(*net.UDPAddr), errors)
	encoder := json.NewEncoder(sender)
	for _, network := range []string{"substituted-network", "shared-network"} {
		if err := encoder.Encode(relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version, FromNetworkID: network, FromNodeID: "peer", Payload: []byte(network)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := wg.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 128)
	n, _, err := wg.ReadFromUDP(buffer)
	if err != nil || string(buffer[:n]) != "shared-network" {
		t.Fatalf("delivered payload %q, err=%v", buffer[:n], err)
	}
}
