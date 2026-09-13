package main

import (
	"slices"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	relayauth "github.com/endless-net/relay/protocol/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNativeCachedMetadataPreservesEndpointsWithoutSecrets(t *testing.T) {
	fixture := newRecoveryTestFixture(t, "https://control.example.test")
	cfg, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnrollmentRecovery = nil
	cfg.ActiveAccountID = "account-selected"
	cfg.Token = "synthetic-session-secret"
	cached := cfg.CachedMap
	cached.Network.AccountID = "account-map"
	cached.Node.Hostname = "native-node"
	cached.Node.AssignedIP = "100.64.0.2"
	cached.Node.AssignedIPv6 = "fd7a:115c:a1e0::2"
	cached.Peers = []clientapi.Peer{{ID: "peer", Hostname: "peer-node", PublicKey: testWireGuardPublicKey("peer"), AllowedIPs: []string{"100.64.0.3/32"}}}
	cached.STUNEndpoints = []clientapi.STUNEndpoint{{ID: "stun", Addr: "127.0.0.1:3478"}}
	cached.Relays = []relayauth.Endpoint{{ID: "relay", Addr: "127.0.0.1:9443", Protocol: relayauth.EndpointProtocolTLS, Priority: 40}}
	cached.MapSignature, err = clientapi.SignNetworkMap(testMapSigningKey(t), *cached)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cached.MapSignature))
	if _, err := verifiedCachedNetworkMap(&cfg); err != nil {
		t.Fatalf("synthetic signed cache invalid: %v", err)
	}
	for _, selected := range []bool{true, false} {
		if !selected {
			cfg.ActiveAccountID = ""
		}
		status := buildAgentRPCStatusWithProbe(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, false)
		wantAccount := "account-map"
		if selected {
			wantAccount = "account-selected"
		}
		if status.AccountId != wantAccount || status.Hostname != "native-node" || status.PeerCount != 1 ||
			!slices.Equal(status.OverlayAddresses, []string{"100.64.0.2", "fd7a:115c:a1e0::2"}) ||
			status.ControlState != ipc.ControlState_CONTROL_STATE_OFFLINE_CACHE || !status.GetStoredState().GetCachedMapValid() {
			t.Fatalf("verified cache metadata was not preserved: state=%s cache_valid=%t account_match=%t hostname_match=%t peers=%d addresses=%d", status.ControlState, status.GetStoredState().GetCachedMapValid(), status.AccountId == wantAccount, status.Hostname == "native-node", status.PeerCount, len(status.OverlayAddresses))
		}
		if status.ServiceState == ipc.ServiceState_SERVICE_STATE_CONNECTED || status.Control != nil {
			t.Fatal("cache-only metadata became live connection evidence")
		}
		if len(status.StunEndpoints) != 1 || status.StunEndpoints[0].Address != "127.0.0.1:3478" ||
			len(status.RelayEndpoints) != 1 || status.RelayEndpoints[0].Address != "127.0.0.1:9443" ||
			status.RelayEndpoints[0].Protocol != relayauth.EndpointProtocolTLS || status.RelayEndpoints[0].Priority != 40 {
			t.Fatal("native status lost signed discovery endpoints")
		}
		raw, err := protojson.Marshal(status)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{cfg.Token, cfg.NodeCredential, cfg.IdentityPrivateKey, cfg.PrivateKey} {
			if secret == "" {
				t.Fatal("redaction fixture requires nonempty synthetic credentials")
			}
			if strings.Contains(string(raw), secret) {
				t.Fatal("native cached metadata exposed synthetic credentials")
			}
		}
	}
}
