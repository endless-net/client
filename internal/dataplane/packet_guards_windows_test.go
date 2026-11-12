//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestWindowsPacketGuardsRejectRouteBypassWithoutBlockingOrdinaryNetwork(t *testing.T) {
	p := policyFixture(t, false)
	for _, layer := range []wf.LayerID{wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		for _, tc := range []struct {
			name, local, remote string
			luid                uint64
			index               uint32
			want                wf.Action
		}{
			{"own path", "100.64.0.1", "100.64.0.2", 12345, 17, wf.ActionPermit},
			{"route moved but local LUID unchanged", "100.64.0.1", "100.64.0.2", 12345, 9, wf.ActionBlock},
			{"recycled interface index", "100.64.0.1", "100.64.0.2", 9, 17, wf.ActionBlock},
			{"new physical source to device", "192.168.80.3", "100.64.0.2", 9, 9, wf.ActionBlock},
			{"old protected source to other peer", "100.64.0.1", "100.64.0.3", 12345, 9, wf.ActionBlock},
			{"unknown packet interface", "100.64.0.1", "100.64.0.2", 12345, 0, wf.ActionBlock},
			{"ordinary internet", "192.168.80.3", "1.1.1.1", 9, 9, 0},
			{"ordinary LAN", "192.168.80.3", "192.168.80.42", 9, 9, 0},
			{"encrypted UDP endpoint", "192.168.80.3", "192.0.2.2", 9, 9, 0},
		} {
			fields := map[wf.FieldID]any{wf.FieldIPLocalAddress: netip.MustParseAddr(tc.local), wf.FieldIPRemoteAddress: netip.MustParseAddr(tc.remote), wf.FieldIPLocalInterface: tc.luid, wf.FieldInterfaceIndex: tc.index}
			if got := policyVerdict(p, layer, fields); got != tc.want {
				t.Fatalf("%s layer%v got%v want%v", tc.name, layer, got, tc.want)
			}
			delete(fields, wf.FieldInterfaceIndex)
			if tc.want != 0 && policyVerdict(p, layer, fields) != wf.ActionBlock {
				t.Fatal("missing interface metadata opened protected scope")
			}
		}
	}
	for _, r := range p.rules {
		if !supportedNativeRule(r) {
			t.Fatal("compiler emitted unsupported native rule")
		}
	}
}

func TestWindowsGuardRetentionDenyAllAndBoundedScopes(t *testing.T) {
	p := policyFixture(t, false)
	m, _ := engineMap(t)
	m.Peers = nil
	withoutPeer, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := filterController(t)
	if _, err := f.Replace(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Replace(context.Background(), withoutPeer); !errors.Is(err, ErrPolicyCleanupRequired) {
		t.Fatal("map change dropped pending resource guard", err)
	}
	_, _, _, journal := ipFixture(t)
	journal.Routes[0].Phase = state.EffectRemoving
	retained, err := withoutPeer.RetainIPGuards(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Replace(context.Background(), retained); err != nil {
		t.Fatal(err)
	}
	fields := map[wf.FieldID]any{wf.FieldIPLocalAddress: netip.MustParseAddr("192.168.80.3"), wf.FieldIPRemoteAddress: netip.MustParseAddr("100.64.0.2"), wf.FieldIPLocalInterface: uint64(9), wf.FieldInterfaceIndex: uint32(9)}
	if policyVerdict(retained, wf.LayerOutboundIPPacketV4, fields) != wf.ActionBlock {
		t.Fatal("removing route lost protection")
	}
	deny := retained.DenyAllTraffic()
	if _, err := f.Replace(context.Background(), deny); err != nil {
		t.Fatal(err)
	}
	fields[wf.FieldIPLocalAddress] = netip.MustParseAddr("100.64.0.1")
	fields[wf.FieldIPLocalInterface] = uint64(12345)
	fields[wf.FieldInterfaceIndex] = uint32(17)
	if policyVerdict(deny, wf.LayerOutboundIPPacketV4, fields) != wf.ActionBlock {
		t.Fatal("cleanup retained a packet exception")
	}
	for _, r := range deny.rules {
		if r.Action == wf.ActionPermit {
			t.Fatal("deny-all retained an application permit")
		}
	}
	tiny := withoutPeer
	tiny.maxProtected = 1
	if _, err := tiny.RetainIPGuards(journal); !errors.Is(err, ErrPolicyLimit) {
		t.Fatal("scope budget ignored", err)
	}
	if len(withoutPeer.scopes) != 1 {
		t.Fatal("retention mutated the original immutable plan")
	}
}

func TestWindowsFilterGuardsCannotCloseUntilAdapterAndRowsAbsent(t *testing.T) {
	f, api := filterController(t)
	p := policyFixture(t, false)
	if _, err := f.Replace(context.Background(), p.DenyAllTraffic()); err != nil {
		t.Fatal(err)
	}
	native := &mockIPAPI{key: p.session}
	ip := &WindowsIP{api: native, maxRows: 128}
	if err := f.CloseAfterIPCleanup(context.Background(), ip); !errors.Is(err, ErrPolicyCleanupRequired) || api.closes != 0 {
		t.Fatal("live adapter lost guards", err)
	}
	native.gone = true
	native.forward = append(native.forward, testIPRoute(12345, "100.64.0.2/32", 7))
	if err := f.CloseAfterIPCleanup(context.Background(), ip); !errors.Is(err, ErrPolicyCleanupRequired) || api.closes != 0 {
		t.Fatal("remaining route lost guards", err)
	}
	native.forward = nil
	if err := f.CloseAfterIPCleanup(context.Background(), ip); err != nil || api.closes != 1 {
		t.Fatal("confirmed cleanup could not release guards", err)
	}
}

func TestWindowsGuardCleanupRequiresDeviceAndRowsAbsentDespitePhantomMIB(t *testing.T) {
	p := policyFixture(t, false)
	native := &mockIPAPI{key: p.session, deviceMissing: true}
	ip := &WindowsIP{api: native, maxRows: 128}
	if err := ip.confirmFilterCleanup(context.Background(), p); err != nil {
		t.Fatal("phantom MIB prevented confirmed device cleanup", err)
	}
	native.deviceErr = ErrAdapterPermission
	if err := ip.confirmFilterCleanup(context.Background(), p); err == nil {
		t.Fatal("unknown device inventory permitted guard cleanup")
	}
	native.deviceErr = nil
	native.forward = []winipcfg.MibIPforwardRow2{testIPRoute(winipcfg.LUID(p.luid), "100.64.0.2/32", 7)}
	if err := ip.confirmFilterCleanup(context.Background(), p); !errors.Is(err, ErrPolicyCleanupRequired) {
		t.Fatal("residual IP route lost guards", err)
	}
	native.forward = nil
	native.key = windows.GUID{}
	if err := ip.confirmFilterCleanup(context.Background(), p); !errors.Is(err, ErrPolicyOwnership) {
		t.Fatal("foreign identity lost guards", err)
	}
}
