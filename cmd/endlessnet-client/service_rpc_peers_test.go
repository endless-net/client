package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/endless-net/client/internal/client"
)

type nativePeerTestEngine struct {
	*testAgentWireGuard
	calls     int
	available bool
	paths     []client.PeerPathStatus
}

func (e *nativePeerTestEngine) TryPathStatus(networkID, nodeID string, revision, globalRevision uint64) ([]client.PeerPathStatus, bool) {
	if networkID != "net-1" || nodeID != "node-1" || revision != 7 || globalRevision != 0 {
		panic("peer source requested paths outside verified map")
	}
	e.calls++
	return e.paths, e.available
}

func TestAgentRPCPeersRequiresVerifiedAppliedMap(t *testing.T) {
	for _, mode := range []string{"current", "tampered", "busy", "foreign-path", "inactive", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
			cfg := client.Config{NodeID: "node-1", NetworkID: "net-1", MapRevision: 7, CachedMap: &networkMap, MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)), RPCState: &client.ClientRPCState{ActiveProfileID: "profile"}}
			if mode == "tampered" {
				cfg.CachedMap.Network.Name = "tampered"
			}
			if mode == "inactive" {
				cfg.RPCState.ActiveProfileID = ""
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			store, err := client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			engine := &nativePeerTestEngine{testAgentWireGuard: &testAgentWireGuard{}, available: mode != "busy"}
			if mode == "foreign-path" {
				engine.paths = []client.PeerPathStatus{{PeerID: "foreign"}}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			out, err := agentRPCPeers(agentIPCOptions{ConfigStore: store, WireGuard: engine})(ctx)
			if mode == "current" {
				if err != nil || out.ProfileID != "profile" || out.MapRevision != 7 || len(out.Peers) != 0 {
					t.Fatal("verified empty catalog unavailable", err)
				}
			} else if err == nil || out.ProfileID != "" || len(out.Peers) != 0 {
				t.Fatal("invalid source exposed current peers")
			}
			wantCalls := 1
			if mode == "tampered" || mode == "inactive" || mode == "cancel" {
				wantCalls = 0
			}
			if engine.calls != wantCalls {
				t.Fatal("invalid map or cancelled request inspected paths")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
