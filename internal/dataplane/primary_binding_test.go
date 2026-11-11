package dataplane

import (
	"testing"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/device"
)

func TestActualPrimaryPeerResetsChangedIdentityAddressAndIntentButRetainsEndpointRefresh(t *testing.T) {
	for _, change := range []string{"node", "address", "intent", "endpoint"} {
		t.Run(change, func(t *testing.T) {
			m, private := engineMap(t)
			m.Peers[0].ID = "peer"
			m.Peers[0].Transport = &coordinator.PeerTransportPlan{PeerAdvertisement: coordinator.TransportAdvertisement{IntentGeneration: 1}}
			e, err := NewEngine(newInertTUN(), &inertBind{}, 4, 51820, engineLease(t))
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			// Lower-level primitive fixture, not a signed map or peer proof.
			if err := e.apply(t.Context(), m, private); err != nil {
				t.Fatal(err)
			}
			actual := e.device.(*device.Device)
			key := peerKey(t, m.Peers[0].PublicKey)
			previous := actual.LookupPeer(key)
			before := e.direct.config.Load().peers[[32]byte(key)].token
			switch change {
			case "node":
				m.Peers[0].ID = "replacement"
			case "address":
				m.Peers[0].AllowedIPs = []string{"100.64.0.3/32"}
			case "intent":
				m.Peers[0].Transport.PeerAdvertisement.IntentGeneration++
			case "endpoint":
				m.Peers[0].Endpoint = "192.0.2.42:51821"
			}
			if err := e.apply(t.Context(), m, private); err != nil {
				t.Fatal(err)
			}
			current := actual.LookupPeer(key)
			if current == nil || change == "endpoint" && current != previous || change != "endpoint" && current == previous {
				t.Fatal("primary peer session did not follow immutable binding")
			}
			if e.direct.config.Load().peers[[32]byte(key)].token == before {
				t.Fatal("old endpoint proof token survived changed binding")
			}
		})
	}
}
