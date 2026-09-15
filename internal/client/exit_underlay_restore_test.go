package client

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExitUnderlayRestoreBindsOnlyAfterContainment(t *testing.T) {
	for _, scenario := range []string{"success", "failure", "cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic-credential", ControlPlaneURLs: []string{"https://control.example"}, ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
			engine := &WireGuardEngine{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fail, cancelContain := scenario == "failure", scenario == "cancellation"
			calls := 0
			guard, err := newLinuxExitGuard("endlessnet", 51820, func(_ context.Context, batch, _ string, _ ...string) ([]byte, error) {
				calls++
				if engine.exitConfig.NodeID != "" {
					t.Error("control identity bound before containment completed")
				}
				if strings.Contains(batch, `output oifname "endlessnet" accept`) {
					t.Error("recovery opened TUN egress")
				}
				if cancelContain {
					cancel()
				}
				if fail {
					return nil, errors.New("injected containment failure")
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = engine.restoreExitUnderlay(ctx, cfg, guard)
			if scenario != "success" {
				if err == nil || engine.exitGuard != guard {
					t.Fatal("uncertain containment lost guard ownership")
				}
				if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
					t.Fatal("unconfirmed containment authorized control traffic")
				}
				other := cfg
				other.NodeCredential = "replacement"
				before := calls
				if err := engine.restoreExitUnderlay(t.Context(), other, guard); err == nil || calls != before {
					t.Fatal("failed recovery allowed a replacement identity")
				}
				fail, cancelContain = false, false
				err = engine.restoreExitUnderlay(t.Context(), cfg, guard)
			}
			if err != nil {
				t.Fatal(err)
			}
			if engine.configured || engine.device != nil || engine.router != nil || engine.exitSelection != nil {
				t.Fatal("underlay recovery claimed applied exit")
			}
			control, err := engine.ControlPlaneHTTPClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			control.CloseIdleConnections()
			before := calls
			other := cfg
			other.NodeCredential = "replacement"
			if err := engine.restoreExitUnderlay(t.Context(), other, guard); err == nil || calls != before {
				t.Fatal("recovery replaced protected identity")
			}
			engine.configured = true
			if err := engine.restoreExitUnderlay(t.Context(), cfg, guard); err == nil || calls != before {
				t.Fatal("recovery changed a running engine")
			}
		})
	}
}
