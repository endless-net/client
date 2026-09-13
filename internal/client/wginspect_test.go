package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRouteInspectionDistinguishesMissingObservationFromOtherInterface(t *testing.T) {
	for _, tc := range []struct {
		name, output, iface string
		failed, uses        bool
	}{
		{name: "empty", failed: true},
		{name: "whitespace", output: "\n \t", failed: true},
		{name: "missing-device", output: "100.64.0.3 via 192.0.2.1", failed: true},
		{name: "truncated-device", output: "100.64.0.3 dev", failed: true},
		{name: "tunnel", output: "100.64.0.3 dev wg0 src 100.64.0.2", iface: "wg0", uses: true},
		{name: "other-interface", output: "100.64.0.3 via 192.0.2.1 dev eth0", iface: "eth0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name != "ip" || strings.Join(args, " ") != "route get 100.64.0.3" {
					t.Fatal("unexpected route lookup command")
				}
				return []byte(tc.output), nil
			}
			got := inspectRoute(t.Context(), runner, "ip", "wg0", "100.64.0.3")
			if calls != 1 || got.Target != "100.64.0.3" || got.Interface != tc.iface || got.UsesInterface != tc.uses || (got.Error != "") != tc.failed {
				t.Fatalf("incorrect route observation: %+v", got)
			}
			if tc.failed && got.Error != "route lookup did not identify an interface" {
				t.Fatal("missing observation must use a fixed failure, not raw output")
			}
		})
	}
}

func TestInspectWireGuardParsesLiveStateWithoutPrivateKey(t *testing.T) {
	privateKey := "secret-interface-private-key"
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		switch key {
		case "wg show wg0 dump":
			return []byte(privateKey + " public-interface-key 51820 off\n" +
				"peer-public-key preshared 192.0.2.10:51820 100.64.0.3/32,10.2.0.0/24 1782260000 1234 5678 25\n"), nil
		case "ip -o link show dev wg0":
			return []byte("7: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n"), nil
		case "ip route get 100.64.0.3":
			return []byte("100.64.0.3 dev wg0 src 100.64.0.2 uid 1000\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command %s", key)
		}
	}

	status := InspectWireGuard(context.Background(), WireGuardInspectOptions{
		Interface:    "wg0",
		RouteTargets: []string{"100.64.0.3"},
		Runner:       runner,
	})
	if !status.OK || status.Interface != "wg0" || status.MTU != 1420 || status.ListenPort != 51820 || status.PeerCount != 1 {
		t.Fatalf("wireguard status = %#v", status)
	}
	peer := status.Peers[0]
	if peer.PublicKey != "peer-public-key" || peer.Endpoint != "192.0.2.10:51820" || peer.LatestHandshakeUnix != 1782260000 || peer.TransferRXBytes != 1234 || peer.TransferTXBytes != 5678 || peer.PersistentKeepaliveSeconds != 25 {
		t.Fatalf("peer status = %#v", peer)
	}
	if strings.Join(peer.AllowedIPs, ",") != "100.64.0.3/32,10.2.0.0/24" {
		t.Fatalf("allowed IPs = %#v", peer.AllowedIPs)
	}
	if len(status.Routes) != 1 || status.Routes[0].Interface != "wg0" || !status.Routes[0].UsesInterface {
		t.Fatalf("routes = %#v", status.Routes)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), privateKey) {
		t.Fatalf("wireguard status leaked interface private key: %s", raw)
	}
}

func TestInspectWireGuardReportsCommandFailure(t *testing.T) {
	status := InspectWireGuard(context.Background(), WireGuardInspectOptions{
		Interface: "wg0",
		Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("Unable to access interface: No such device\n"), fmt.Errorf("exit status 1")
		},
	})
	if status.OK || !strings.Contains(status.Error, "No such device") {
		t.Fatalf("wireguard failure status = %#v", status)
	}
}
