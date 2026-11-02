//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wireguard-go/conn"
)

func TestWindowsSessionRejectsMissingOrRenewedOperationBeforeNativeEffects(t *testing.T) {
	for _, failure := range []string{"missing", "renewed", "completed"} {
		s, _, _, m, ip, _, _, _, clock := wiredSessionFixture(t)
		ctx := context.Background()
		if failure != "missing" {
			if failure == "renewed" {
				clock.advance(time.Second)
			}
			receipt, _ := clock.Sample()
			b, err := connectivity.NewOperationBudget(clock, s.profileID, s.generation, receipt, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "completed" {
				if err := b.Complete(); err != nil {
					t.Fatal(err)
				}
			}
			ctx = connectivity.WithOperationBudget(ctx, b)
		}
		if err := s.Start(ctx, m); !errors.Is(err, connectivity.ErrConfig) || ip.createdAddresses+ip.createdRoutes != 0 || s.lease.Permissions().ExistingFlows {
			t.Fatal("invalid operation reached native effects", failure, err)
		}
	}
}

func TestWindowsSessionSuspendDuringFinalBindCannotActivatePackets(t *testing.T) {
	s, _, _, m, _, dynamic, _, _, clock := wiredSessionFixture(t)
	operation, b := sessionFixtureOperation(t, s, context.Background(), false)
	s.makeBind = func() conn.Bind {
		return &sessionMutatingBind{inertBind: &inertBind{}, mutate: func() { clock.advance(31 * time.Second) }}
	}
	if err := s.Start(operation, m); !errors.Is(err, context.DeadlineExceeded) || s.lease.Permissions().NewFlows || s.lease.Permissions().ExistingFlows || len(dynamic.live) != 0 {
		t.Fatal("late native result enabled traffic", err)
	}
	if !errors.Is(b.Complete(), context.DeadlineExceeded) {
		t.Fatal("late path decision restored expired operation")
	}
}

func TestWindowsSessionLocalStartAloneDoesNotEndOperationPacketGuard(t *testing.T) {
	s, _, _, m, _, _, _, _, clock := wiredSessionFixture(t)
	operation, _ := sessionFixtureOperation(t, s, context.Background(), false)
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	if !s.lease.Permissions().NewFlows {
		t.Fatal("operation did not permit initial Direct confirmation")
	}
	clock.advance(30 * time.Second)
	if s.lease.Permissions().NewFlows || s.lease.Permissions().ExistingFlows {
		t.Fatal("local application stood in for timely Direct confirmation")
	}
}
