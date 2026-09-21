package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitStartupSeparatesContainedRecoveryFromFailedProtection(t *testing.T) {
	for _, scenario := range []string{"authority", "dns", "authority_and_containment_failure", "containment_readback_failure", "capture_cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			contained := false
			engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet", underlayDNSCapture: func(ctx context.Context, name string) (*underlayDNSSource, error) {
				if !contained {
					t.Fatal("DNS captured before containment")
				}
				if scenario == "capture_cancellation" {
					cancel()
					return nil, context.Canceled
				}
				return nil, errors.New("source unavailable")
			}}}
			if scenario == "authority" || scenario == "authority_and_containment_failure" {
				cfg.NodeCredential = ""
			}
			create := func(name, table string) (*linuxExitGuard, error) {
				runner := exitGuardReadbackRunner(t, name, 51820, func(context.Context, string, string, ...string) ([]byte, error) {
					if scenario == "authority_and_containment_failure" {
						return nil, errors.New("containment rejected")
					}
					contained = true
					return nil, nil
				})
				return newLinuxExitGuard(name, 51820, func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
					if scenario == "containment_readback_failure" && input == "" {
						return nil, errors.New("readback unavailable")
					}
					return runner(ctx, input, command, args...)
				})
			}
			err := engine.restoreStartupExit(ctx, cfg, create)
			if err == nil {
				t.Fatal("unavailable recovery succeeded")
			}
			wantRecoverable := scenario == "authority" || scenario == "dns"
			if ExitProtectionRestoredWithoutAuthority(err) != wantRecoverable {
				t.Fatalf("unexpected startup classification: %v", err)
			}
			if ExitProtectionRestoredWithoutAuthority(fmt.Errorf("startup: %w", err)) != wantRecoverable {
				t.Fatal("wrapping changed startup classification")
			}
			if engine.exitGuard == nil || engine.exitSelection != nil || engine.exitConfig.NodeID != "" || engine.underlayDNS != nil || engine.configured {
				t.Fatal("failed recovery lost protection or granted authority")
			}
			if control, controlErr := engine.ControlPlaneHTTPClient(cfg); controlErr == nil || control != nil {
				t.Fatal("local recovery classification granted control traffic")
			}
			if scenario == "capture_cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatal("startup cancellation lost")
			}
		})
	}
}

func TestExitStartupAuthorityRetryKeepsOriginalScope(t *testing.T) {
	cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
	available := false
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet", underlayDNSCapture: func(ctx context.Context, name string) (*underlayDNSSource, error) {
		if !available {
			return nil, errors.New("resolver unavailable")
		}
		return testUnderlayDNSCapture(ctx, name)
	}}}
	created := 0
	create := func(name, _ string) (*linuxExitGuard, error) {
		created++
		return newLinuxExitGuard(name, 51820, exitGuardReadbackRunner(t, name, 51820, func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }))
	}
	if err := engine.restoreStartupExit(t.Context(), cfg, create); !ExitProtectionRestoredWithoutAuthority(err) {
		t.Fatal("unavailable resolver prevented protected local recovery", err)
	}
	guard := engine.exitGuard
	changed := cfg
	changed.NodeCredential = "replacement"
	available = true
	if err := engine.restoreStartupExit(t.Context(), changed, create); !ExitProtectionRestoredWithoutAuthority(err) || engine.exitConfig.NodeID != "" {
		t.Fatal("retry replaced reserved identity", err)
	}
	if err := engine.restoreStartupExit(t.Context(), cfg, create); err != nil {
		t.Fatal(err)
	}
	if created != 1 || engine.exitGuard != guard || engine.exitSelection != nil || engine.configured {
		t.Fatal("retry replaced scope or claimed an applied tunnel")
	}
	control, err := engine.ControlPlaneHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	control.CloseIdleConnections()
}

func TestExitStartupRecoveryClassificationRejectsOrdinaryErrors(t *testing.T) {
	for _, err := range []error{nil, errors.New("containment unavailable"), context.Canceled, context.DeadlineExceeded} {
		if ExitProtectionRestoredWithoutAuthority(err) {
			t.Fatal("ordinary startup failure admitted local recovery")
		}
	}
}
