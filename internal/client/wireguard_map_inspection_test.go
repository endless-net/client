package client

import (
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestWireGuardMapInspectionRequiresCurrentAppliedIdentity(t *testing.T) {
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet", router: &testWireGuardEngineRouter{},
		tunFactory: func(string, int) (tun.Device, error) { return tuntest.NewChannelTUN().TUN(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if _, available := engine.TryMapInspection("network", "node", 9, 0); available {
		t.Fatal("unconfigured engine claimed a map")
	}
	networkMap := api.RegisterNodeResponse{
		Network: api.Network{ID: "network", Name: "default", CIDR: "100.64.0.0/24", Revision: 9},
		Node:    api.Node{ID: "node", Hostname: "node", NetworkID: "network", PublicKey: testWireGuardEnginePublicKey(1), AssignedIP: "100.64.0.2"},
	}
	if _, err := engine.Configure(t.Context(), Config{PrivateKey: testWireGuardEngineKey(1)}, networkMap); err != nil {
		t.Fatal(err)
	}
	for _, query := range []struct {
		network, node string
		revision      uint64
	}{{"", "node", 9}, {"network", "", 9}, {"network", "node", 0}, {"foreign", "node", 9}, {"network", "foreign", 9}, {"network", "node", 8}, {"network", "node", 10}} {
		if _, available := engine.TryMapInspection(query.network, query.node, query.revision, 0); available {
			t.Fatal("inspection accepted an absent, foreign or stale map identity")
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		inspection, available := engine.TryMapInspection("network", "node", 9, 0)
		if available {
			if !inspection.OK || inspection.ListenPort == 0 {
				t.Fatal("current map did not return a live engine inspection")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("current map inspection remained unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	engine.mu.Lock()
	engine.pathMap.Revision.Global = 12
	engine.mu.Unlock()
	if _, available := engine.TryMapInspection("network", "node", 9, 11); available {
		t.Fatal("inspection accepted old global authorization revision")
	}
	// Verify the positive case without racing the path worker's nonblocking lock.
	deadline = time.Now().Add(time.Second)
	for {
		if _, available := engine.TryMapInspection("network", "node", 9, 12); available {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("matching global revision unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	engine.mu.Lock()
	_, available := engine.TryMapInspection("network", "node", 9, 12)
	engine.mu.Unlock()
	if available {
		t.Fatal("busy engine claimed an observation")
	}
	if _, err := engine.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, available := engine.TryMapInspection("network", "node", 9, 12); available {
		t.Fatal("stopped engine retained a connected map observation")
	}
}
