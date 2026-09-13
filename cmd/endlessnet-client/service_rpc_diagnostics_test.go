package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/endless-net/client/internal/client"
)

type nonblockingDiagnosticsEngine struct {
	*testAgentWireGuard
	calls int
}

func (e *nonblockingDiagnosticsEngine) Inspection() client.WireGuardInspection {
	panic("blocking inspection must not be called")
}
func (e *nonblockingDiagnosticsEngine) TryInspection() (client.WireGuardInspection, bool) {
	e.calls++
	return client.WireGuardInspection{}, false
}

func TestAgentRPCDiagnosticsVerifiesMapAndDoesNotWaitForEngine(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid-map", true: "verified-map"}[valid], func(t *testing.T) {
			networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
			cfg := client.Config{NodeID: "node-1", NetworkID: "net-1", MapRevision: 7, CachedMap: &networkMap, MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature))}
			if !valid {
				cfg.CachedMap.Network.Name = "tampered"
			}
			path := filepath.Join(t.TempDir(), "client.json")
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			store, err := client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			engine := &nonblockingDiagnosticsEngine{testAgentWireGuard: &testAgentWireGuard{}}
			provider := agentRPCDiagnostics(agentIPCOptions{ConfigStore: store, WireGuard: engine, WGInterface: "endlessnet"})
			out, err := provider(t.Context())
			if err != nil || !out.TunnelBusy || engine.calls != 1 {
				t.Fatal("busy engine blocked or was misreported", err)
			}
			if out.VerifiedMap != valid || (out.DNS != nil) != valid {
				t.Fatal("DNS source verification boundary failed")
			}
			if !valid && len(out.RouteConflicts) != 0 {
				t.Fatal("unverified map conflicts leaked")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = provider(ctx)
			if !errors.Is(err, context.Canceled) || engine.calls != 1 {
				t.Fatal("cancelled diagnostics probed engine", err)
			}
		})
	}
}
