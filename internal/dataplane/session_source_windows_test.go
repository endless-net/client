//go:build windows

package dataplane

import (
	"context"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/conn"
)

func TestSourceChangeDuringNativeBindRetainsResourcesWithoutLateTraffic(t *testing.T) {
	s, _, _, m, ip, _, _, _, clock := wiredSessionFixture(t)
	operation, b := sessionFixtureOperation(t, s, context.Background(), false)
	var nextEpoch uint64
	s.makeBind = func() conn.Bind {
		return &sessionMutatingBind{inertBind: &inertBind{}, mutate: func() { nextEpoch = s.BlockSource() }}
	}
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	if s.lease.Permissions().NewFlows || s.lease.Permissions().ExistingFlows || ip.createdAddresses != 1 || s.current.Cursor() != m.Cursor() {
		t.Fatal("stale native completion reopened source gate or forgot receipts")
	}
	if s.supervisor == nil {
		t.Fatal("superseded first apply lost the session's transport supervisor")
	}
	originalSupervisor := s.supervisor
	clock.advance(time.Second)
	next := nextSessionMap(t, s, m, clock, func(*coordinator.NetworkMapSnapshot) {})
	if err := s.Refresh(WithSourceEpoch(context.Background(), nextEpoch), next); err != nil {
		t.Fatal(err)
	}
	if !s.lease.Permissions().NewFlows || ip.createdAddresses != 1 || s.supervisor != originalSupervisor {
		t.Fatal("fresh source did not resume same owned resources")
	}
	if err := b.Complete(); err != nil {
		t.Fatal(err)
	}
}
