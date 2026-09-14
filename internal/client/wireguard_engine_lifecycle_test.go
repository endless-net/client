package client

import (
	"context"
	"errors"
	"testing"

	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

type lifecycleEngineRouter struct {
	testWireGuardEngineRouter
	downErr error
}

func (r *lifecycleEngineRouter) Down(context.Context) error {
	r.down++
	return r.downErr
}

func TestWireGuardSuspendBlocksApplyUntilExplicitResume(t *testing.T) {
	for _, scenario := range []string{"running", "stopped", "cleanup_failure", "cancelled_resume"} {
		t.Run(scenario, func(t *testing.T) {
			router := &lifecycleEngineRouter{}
			created := 0
			engine, err := NewWireGuardEngine(WireGuardEngineOptions{
				Interface:  "endlessnet",
				tunFactory: func(string, int) (tun.Device, error) { created++; return tuntest.NewChannelTUN().TUN(), nil },
				router:     router,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { router.downErr = nil; _ = engine.Close() }()
			cfg, networkMap := Config{PrivateKey: testWireGuardEngineKey(61)}, testWireGuardEngineNetworkMap(61, 62)
			if scenario != "stopped" {
				if _, err := engine.Configure(t.Context(), cfg, networkMap); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cleanup_failure" {
				router.downErr = errors.New("routes still installed")
			}
			result, err := engine.Suspend(t.Context())
			if scenario == "cleanup_failure" {
				if !errors.Is(err, router.downErr) || result.OK || engine.router == nil {
					t.Fatal("failed suspension lost route cleanup", result, err)
				}
			} else if err != nil || !result.OK {
				t.Fatal(result, err)
			}
			before := created
			if result, err := engine.Configure(t.Context(), cfg, networkMap); !errors.Is(err, ErrWireGuardRuntimeSuspended) || result.OK || created != before {
				t.Fatal("suspend allowed a fresh apply", result, err)
			}
			if engine.Inspection().OK {
				t.Fatal("suspended engine reported a live tunnel")
			}
			if scenario == "cleanup_failure" {
				if result, err := engine.Down(t.Context()); !errors.Is(err, router.downErr) || result.OK {
					t.Fatal("repeated Down fabricated route cleanup", result, err)
				}
				if err := engine.Resume(t.Context()); !errors.Is(err, router.downErr) {
					t.Fatal("resume ignored failed route removal", err)
				}
				if _, err := engine.Configure(t.Context(), cfg, networkMap); !errors.Is(err, ErrWireGuardRuntimeSuspended) {
					t.Fatal("failed resume released gate", err)
				}
				router.downErr = nil
			}
			if scenario == "cancelled_resume" {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if err := engine.Resume(ctx); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if _, err := engine.Configure(t.Context(), cfg, networkMap); !errors.Is(err, ErrWireGuardRuntimeSuspended) {
					t.Fatal("cancelled resume released gate", err)
				}
			}
			if err := engine.Resume(t.Context()); err != nil {
				t.Fatal(err)
			}
			if created != before || engine.Inspection().OK {
				t.Fatal("resume reapplied an old map without runtime authorization")
			}
			if err := engine.Resume(t.Context()); err != nil {
				t.Fatal("duplicate resume", err)
			}
			if result, err := engine.Configure(t.Context(), cfg, networkMap); err != nil || !result.OK || created != before+1 {
				t.Fatal("explicit authorized apply after resume failed", result, err)
			}
		})
	}
}

func TestWireGuardConfigureCannotReplaceFailedRouteCleanup(t *testing.T) {
	router := &lifecycleEngineRouter{}
	created := 0
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface:  "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) { created++; return tuntest.NewChannelTUN().TUN(), nil },
		router:     router,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { router.downErr = nil; _ = engine.Close() }()
	cfg, networkMap := Config{PrivateKey: testWireGuardEngineKey(61)}, testWireGuardEngineNetworkMap(61, 62)
	if _, err := engine.Configure(t.Context(), cfg, networkMap); err != nil {
		t.Fatal(err)
	}
	router.downErr = errors.New("routes still installed")
	if result, err := engine.Down(t.Context()); !errors.Is(err, router.downErr) || result.OK {
		t.Fatal(result, err)
	}
	if result, err := engine.Configure(t.Context(), cfg, networkMap); !errors.Is(err, router.downErr) || result.OK || created != 1 {
		t.Fatal("new apply abandoned failed cleanup", result, err)
	}
	router.downErr = nil
	if result, err := engine.Down(t.Context()); err != nil || !result.OK || result.Skipped || router.down != 3 {
		t.Fatal("route-only cleanup was skipped", result, err, router.down)
	}
}
