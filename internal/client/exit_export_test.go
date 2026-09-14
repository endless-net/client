package client

import (
	"reflect"
	"strings"
	"testing"
)

func TestStaticExportDoesNotActivateImplicitExit(t *testing.T) {
	cfg, networkMap, signingKey := signedApplicationFixture(t, false)
	cfg.PrivateKey = testWireGuardEngineKey(1)
	networkMap.Network.Applications = nil
	networkMap.Peers[0].AllowedIPs = []string{"100.64.0.3/32", "10.1.0.0/16", "0.0.0.0/0", "::/0"}
	resignApplicationMap(t, &networkMap, signingKey)
	before := cloneRegisterNodeResponse(networkMap)
	for _, blockLAN := range []bool{false, true} {
		for _, managed := range []bool{false, true} {
			rendered, err := RenderWireGuardWithOptionsChecked(cfg, networkMap, WireGuardRenderOptions{
				ExitBlockLAN: blockLAN, sharingPacketEnforcement: managed,
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered, "0.0.0.0/0") || strings.Contains(rendered, "::/0") || strings.Contains(rendered, "ENLAN-") {
				t.Fatal("static export activated implicit exit or its LAN hooks")
			}
			if !strings.Contains(rendered, "AllowedIPs = 100.64.0.3/32, 10.1.0.0/16") {
				t.Fatal("static export lost ordinary routes")
			}
		}
	}
	if !reflect.DeepEqual(before, networkMap) {
		t.Fatal("static export mutated the signed source")
	}
}
