package client

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestEngineDoesNotActivateImplicitExitRoutes(t *testing.T) {
	networkMap := api.RegisterNodeResponse{
		Network: api.Network{ID: "network", Name: "network", CIDR: "100.64.0.0/24", Revision: 1},
		Node:    api.Node{ID: "self", AssignedIP: "100.64.0.2", PublicKey: testWireGuardEnginePublicKey(1)},
		Peers:   []api.Peer{{ID: "peer", PublicKey: testWireGuardEnginePublicKey(2), AllowedIPs: []string{"100.64.0.3/32", "10.1.0.0/16", "0.0.0.0/0", "::/0"}}},
	}
	before := cloneRegisterNodeResponse(networkMap)
	routes, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, Config{}, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes.Routes) != 2 {
		t.Fatal("ordinary routes lost or implicit defaults installed")
	}
	for _, route := range routes.Routes {
		if route.Bits() == 0 {
			t.Fatal("implicit OS default route")
		}
	}
	for _, emitEndpoints := range []bool{false, true} {
		for _, configured := range []bool{false, true} {
			wire, err := wireGuardEngineUAPIWithEndpoints(testWireGuardEngineKey(1), networkMap, 0, configured, 0, map[string]string{"peer": "127.0.0.1:1234"}, emitEndpoints)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(wire, "allowed_ip=0.0.0.0/0") || strings.Contains(wire, "allowed_ip=::/0") || !strings.Contains(wire, "allowed_ip=100.64.0.3/32") || !strings.Contains(wire, "allowed_ip=10.1.0.0/16") {
				t.Fatal("UAPI activated implicit exit or removed ordinary routes")
			}
		}
	}
	if !reflect.DeepEqual(before, networkMap) {
		t.Fatal("engine projection changed signed source")
	}
}
