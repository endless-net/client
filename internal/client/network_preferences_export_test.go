package client

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"google.golang.org/protobuf/proto"
)

func TestStaticExportRespectsNetworkAcceptance(t *testing.T) {
	for _, mode := range []string{"default", "local_disabled", "managed_disabled", "locked_enabled", "tampered", "missing_trust"} {
		t.Run(mode, func(t *testing.T) {
			opts, key := signedServiceDNSFixture(t)
			source := opts.NetworkMap
			source.Network.DNS = []string{"100.64.0.53"}
			source.Peers[0].AllowedIPs = []string{"100.64.0.2/32", "fd00::2/128", "198.18.0.0/15", "192.0.2.1/32", "0.0.0.0/0", "::/0"}
			source.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceSubnet, ID: source.Peers[0].ID, CIDR: "192.0.2.1/32", Source: api.ClientPolicyAccount, PolicyID: "subnet", Enabled: true}}}
			cfg := Config{PrivateKey: testWireGuardEngineKey(1), NodeID: source.Node.ID, NetworkID: source.Network.ID, MapSigningTrust: opts.SigningTrust}
			if mode == "local_disabled" || mode == "locked_enabled" {
				cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(false), AcceptRoutes: proto.Bool(false)}
			}
			if mode == "managed_disabled" || mode == "locked_enabled" {
				for _, setting := range []api.ClientSettingKey{api.ClientSettingAcceptDNS, api.ClientSettingAcceptRoutes} {
					source.Network.ClientPolicy.Settings = append(source.Network.ClientPolicy.Settings, api.ManagedClientSetting{Key: setting, Source: api.ClientPolicyDevice, PolicyID: "acceptance", BooleanValue: proto.Bool(mode == "locked_enabled"), Locked: true})
				}
			}
			resignApplicationMap(t, &source, key)
			if mode == "tampered" {
				source.Network.DNS = []string{"100.64.0.54"}
			}
			if mode == "missing_trust" {
				cfg.MapSigningTrust = nil
			}
			before := cloneRegisterNodeResponse(source)
			rendered, err := RenderWireGuardWithOptionsChecked(cfg, source, WireGuardRenderOptions{})
			if !reflect.DeepEqual(before, source) {
				t.Fatal("export changed signed source")
			}
			if mode == "tampered" || mode == "missing_trust" {
				if err == nil || rendered != "" {
					t.Fatal("untrusted policy exported configuration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered, "0.0.0.0/0") || strings.Contains(rendered, "::/0") {
				t.Fatal("implicit exit returned")
			}
			if !strings.Contains(rendered, "100.64.0.2/32") || !strings.Contains(rendered, "fd00::2/128") {
				t.Fatal("ordinary host addresses omitted")
			}
			want := mode != "local_disabled" && mode != "managed_disabled"
			for _, value := range []string{"DNS = 100.64.0.53", "198.18.0.0/15", "192.0.2.1/32"} {
				if strings.Contains(rendered, value) != want {
					t.Fatal("export ignored effective preference", value)
				}
			}
		})
	}
}
