package client

import (
	"context"
	"errors"
	"fmt"
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
				if err := engine.restoreExitUnderlay(t.Context(), other, guard); err == nil || calls != before+1 || engine.exitRestoreConfig.NodeCredential != cfg.NodeCredential {
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
			if err := engine.restoreExitUnderlay(t.Context(), other, guard); err == nil || calls != before+1 || engine.exitRestoreConfig.NodeCredential != cfg.NodeCredential {
				t.Fatal("recovery replaced protected identity")
			}
			before = calls
			engine.configured = true
			if err := engine.restoreExitUnderlay(t.Context(), cfg, guard); err == nil || calls != before {
				t.Fatal("recovery changed a running engine")
			}
		})
	}
}

func TestExitRecoveryRetainsTableBindingAcrossRetry(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, WireGuardRouteTable: "51999", ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
			cfg.ExitSelection.RouteTable = cfg.WireGuardRouteTable
			engine := &WireGuardEngine{}
			calls := 0
			guard, err := newLinuxExitGuard("endlessnet", 51999, func(context.Context, string, string, ...string) ([]byte, error) {
				calls++
				if failed && calls == 1 {
					return nil, errors.New("containment failed")
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.restoreExitUnderlay(t.Context(), cfg, guard); (err != nil) != failed {
				t.Fatal("unexpected first recovery", err)
			}
			other := cfg
			other.WireGuardRouteTable = "52000"
			if err := engine.restoreExitUnderlay(t.Context(), other, guard); err == nil || calls != 2 || engine.exitGuard != guard || engine.exitRestoreConfig.WireGuardRouteTable != "51999" {
				t.Fatal("recovery rebound the owned guard to a different table", err)
			}
			if control, err := engine.ControlPlaneHTTPClient(other); err == nil || control != nil {
				t.Fatal("changed table obtained old guard's control authority")
			}
			if err := engine.restoreExitUnderlay(t.Context(), cfg, guard); err != nil || calls != 3 {
				t.Fatal("original table could not recover", err)
			}
			control, err := engine.ControlPlaneHTTPClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			control.CloseIdleConnections()
		})
	}
}
