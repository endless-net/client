//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type mockIPAPI struct {
	deviceMissing                                                    bool
	deviceErr                                                        error
	gone                                                             bool
	key                                                              windows.GUID
	addrs                                                            []winipcfg.MibUnicastIPAddressRow
	forward                                                          []winipcfg.MibIPforwardRow2
	createdAddresses, createdRoutes, deletedAddresses, deletedRoutes int
	loseCreate, loseDelete                                           bool
	afterCreate                                                      func()
}

func (m *mockIPAPI) devicePresent(ctx context.Context, _ windows.GUID, _ int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return !m.gone && !m.deviceMissing, m.deviceErr
}

func (m *mockIPAPI) guid(winipcfg.LUID) (*windows.GUID, error) {
	if m.gone {
		return nil, windows.ERROR_NOT_FOUND
	}
	key := m.key
	return &key, nil
}
func (m *mockIPAPI) addresses() ([]winipcfg.MibUnicastIPAddressRow, error) {
	return append([]winipcfg.MibUnicastIPAddressRow(nil), m.addrs...), nil
}
func (m *mockIPAPI) routes() ([]winipcfg.MibIPforwardRow2, error) {
	return append([]winipcfg.MibIPforwardRow2(nil), m.forward...), nil
}
func (m *mockIPAPI) address(l winipcfg.LUID, a netip.Addr) (*winipcfg.MibUnicastIPAddressRow, error) {
	for _, r := range m.addrs {
		if r.InterfaceLUID == l && r.Address.Addr() == a {
			v := r
			return &v, nil
		}
	}
	return nil, windows.ERROR_NOT_FOUND
}
func (m *mockIPAPI) route(l winipcfg.LUID, p netip.Prefix, n netip.Addr) (*winipcfg.MibIPforwardRow2, error) {
	for _, r := range m.forward {
		if r.InterfaceLUID == l && r.DestinationPrefix.Prefix() == p && r.NextHop.Addr() == n {
			v := r
			return &v, nil
		}
	}
	return nil, windows.ERROR_NOT_FOUND
}
func (m *mockIPAPI) createAddress(row *winipcfg.MibUnicastIPAddressRow) error {
	m.createdAddresses++
	r := *row
	r.CreationTimeStamp = 100
	r.DadState = winipcfg.DadStateTentative
	r.InterfaceIndex = 17
	m.addrs = append(m.addrs, r)
	auto := testIPRoute(row.InterfaceLUID, netip.PrefixFrom(row.Address.Addr(), 32).String(), 9)
	auto.Protocol = winipcfg.RouteProtocolLocal
	auto.Loopback = true
	auto.Origin = winipcfg.RouteOriginWellKnown
	m.forward = append(m.forward, auto)
	if m.afterCreate != nil {
		m.afterCreate()
	}
	if m.loseCreate {
		return errors.New("lost native acknowledgement")
	}
	return nil
}
func (m *mockIPAPI) createRoute(row *winipcfg.MibIPforwardRow2) error {
	m.createdRoutes++
	r := *row
	r.Origin = winipcfg.RouteOriginManual
	r.InterfaceIndex = 17
	m.forward = append(m.forward, r)
	if m.afterCreate != nil {
		m.afterCreate()
	}
	if m.loseCreate {
		return errors.New("lost native acknowledgement")
	}
	return nil
}
func (m *mockIPAPI) deleteAddress(row *winipcfg.MibUnicastIPAddressRow) error {
	m.deletedAddresses++
	for i, r := range m.addrs {
		if r.InterfaceLUID == row.InterfaceLUID && r.Address.Addr() == row.Address.Addr() {
			m.addrs = append(m.addrs[:i], m.addrs[i+1:]...)
			break
		}
	}
	for i, r := range m.forward {
		if r.InterfaceLUID == row.InterfaceLUID && r.Protocol == winipcfg.RouteProtocolLocal && r.DestinationPrefix.Prefix() == netip.PrefixFrom(row.Address.Addr(), 32) {
			m.forward = append(m.forward[:i], m.forward[i+1:]...)
			break
		}
	}
	if m.loseDelete {
		return errors.New("lost native delete acknowledgement")
	}
	return nil
}
func (m *mockIPAPI) deleteRoute(row *winipcfg.MibIPforwardRow2) error {
	m.deletedRoutes++
	for i, r := range m.forward {
		if r.InterfaceLUID == row.InterfaceLUID && r.DestinationPrefix.Prefix() == row.DestinationPrefix.Prefix() && r.NextHop.Addr() == row.NextHop.Addr() {
			m.forward = append(m.forward[:i], m.forward[i+1:]...)
			break
		}
	}
	if m.loseDelete {
		return errors.New("lost native delete acknowledgement")
	}
	return nil
}
func testIPRoute(l winipcfg.LUID, prefix string, metric uint32) winipcfg.MibIPforwardRow2 {
	var r winipcfg.MibIPforwardRow2
	r.Init()
	r.InterfaceLUID = l
	r.Metric = metric
	r.ValidLifetime = ^uint32(0)
	r.PreferredLifetime = ^uint32(0)
	_ = r.DestinationPrefix.SetPrefix(netip.MustParsePrefix(prefix))
	_ = r.NextHop.SetAddr(netip.IPv4Unspecified())
	return r
}
func ipFixture(t *testing.T) (*WindowsIP, *mockIPAPI, IPPlan, state.ResourceJournal) {
	t.Helper()
	m, _ := engineMap(t)
	p, err := ipPlan(m, policySession, 12345, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	a, routes := p.Prepared()
	j := state.ResourceJournal{ID: policySession, Generation: 1, Adapter: &state.AdapterRecord{ID: policySession, Name: "unit adapter", LUID: 12345, EffectStamp: state.EffectStamp{Phase: state.EffectApplied, Fingerprint: strings.Repeat("a", 64)}}, Addresses: []state.AddressRecord{a}, Routes: routes}
	api := &mockIPAPI{key: p.adapter}
	return &WindowsIP{api: api, maxRows: 128}, api, p, j
}
func removingAddress(r state.AddressRecord) state.AddressRecord {
	r.Phase = state.EffectRemoving
	if r.AutomaticRoute != nil {
		v := *r.AutomaticRoute
		v.Phase = state.EffectRemoving
		r.AutomaticRoute = &v
	}
	return r
}

func TestWindowsIPIndividualEffectsDADAndCleanupPreserveForeignRows(t *testing.T) {
	w, api, p, j := ipFixture(t)
	foreign := []winipcfg.MibIPforwardRow2{testIPRoute(9, "0.0.0.0/0", 10), testIPRoute(9, "192.168.80.0/24", 1)}
	api.forward = append(api.forward, foreign...)
	if err := w.Preflight(context.Background(), p, j); err != nil {
		t.Fatal(err)
	}
	effect, err := w.CreateAddress(context.Background(), p, j)
	if err != nil || effect.Ready || effect.Record.Fingerprint == "" || effect.Record.AutomaticRoute == nil {
		t.Fatal("create is not completed DAD", err)
	}
	j.Addresses[0] = effect.Record
	api.addrs[0].DadState = winipcfg.DadStatePreferred
	ready, err := w.InspectAddress(context.Background(), p, effect.Record)
	if err != nil || !ready.Ready || ready.Record.Fingerprint != effect.Record.Fingerprint {
		t.Fatal("DAD refresh changed ownership", err)
	}
	receipt, err := w.CreateRoute(context.Background(), p, j.Routes[0], j)
	if err != nil || receipt.Fingerprint == "" || receipt.Phase != state.EffectApplied {
		t.Fatal(err)
	}
	cleanup, err := PrepareIPCleanup(policySession, uint64(p.luid), effect.Record.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateAddress(context.Background(), cleanup, j); !errors.Is(err, ErrConfiguration) {
		t.Fatal("cleanup context created an address", err)
	}
	if _, err := w.CreateRoute(context.Background(), cleanup, j.Routes[0], j); !errors.Is(err, ErrConfiguration) {
		t.Fatal("cleanup context created a route", err)
	}
	receipt.Phase = state.EffectRemoving
	if err := w.RemoveRoute(context.Background(), cleanup, receipt); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveAddress(context.Background(), cleanup, removingAddress(effect.Record)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(api.forward, foreign) || len(api.addrs) != 0 || api.createdAddresses != 1 || api.createdRoutes != 1 || api.deletedAddresses != 1 || api.deletedRoutes != 1 {
		t.Fatal("cleanup changed foreign tables or left own effect")
	}
	if err := w.RemoveRoute(context.Background(), p, receipt); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveAddress(context.Background(), p, removingAddress(effect.Record)); err != nil {
		t.Fatal(err)
	}
	if api.deletedAddresses != 1 || api.deletedRoutes != 1 {
		t.Fatal("absence triggered another delete")
	}
}

func TestWindowsIPConflictsBeforeAnyCreate(t *testing.T) {
	for _, which := range []string{"LAN", "foreign host route", "foreign local address", "unowned same interface", "limit", "adapter replaced"} {
		t.Run(which, func(t *testing.T) {
			w, api, p, j := ipFixture(t)
			switch which {
			case "LAN":
				api.forward = append(api.forward, testIPRoute(9, "100.64.0.0/10", 1))
			case "foreign host route":
				api.forward = append(api.forward, testIPRoute(9, "100.64.0.2/32", 1))
			case "foreign local address":
				r := p.address
				r.InterfaceLUID = 9
				api.addrs = append(api.addrs, r)
			case "unowned same interface":
				r := p.address
				api.addrs = append(api.addrs, r)
			case "limit":
				w.maxRows = 1
				api.forward = append(api.forward, testIPRoute(9, "0.0.0.0/0", 1), testIPRoute(9, "192.168.0.0/24", 1))
			case "adapter replaced":
				api.key.Data1++
			}
			if _, err := w.CreateAddress(context.Background(), p, j); err == nil || api.createdAddresses != 0 || api.createdRoutes != 0 {
				t.Fatal("conflict applied effects", err)
			}
		})
	}
}

func TestWindowsIPChangedObjectsAreNeverDeleted(t *testing.T) {
	for _, which := range []string{"creation identity", "address flags", "automatic route", "peer route"} {
		t.Run(which, func(t *testing.T) {
			w, api, p, j := ipFixture(t)
			effect, err := w.CreateAddress(context.Background(), p, j)
			if err != nil {
				t.Fatal(err)
			}
			j.Addresses[0] = effect.Record
			route, err := w.CreateRoute(context.Background(), p, j.Routes[0], j)
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "creation identity":
				api.addrs[0].CreationTimeStamp++
			case "address flags":
				api.addrs[0].SkipAsSource = true
			case "automatic route":
				api.forward[0].Metric++
			case "peer route":
				api.forward[1].Metric++
			}
			if which == "peer route" {
				route.Phase = state.EffectRemoving
				err = w.RemoveRoute(context.Background(), p, route)
			} else {
				err = w.RemoveAddress(context.Background(), p, removingAddress(effect.Record))
			}
			if !errors.Is(err, ErrIPOwnership) || api.deletedAddresses+api.deletedRoutes != 0 {
				t.Fatal("changed object deleted", err)
			}
		})
	}
}

func TestWindowsIPLostRepliesDoNotAdoptOrSilentlyRetry(t *testing.T) {
	w, api, p, j := ipFixture(t)
	api.loseCreate = true
	if _, err := w.CreateAddress(context.Background(), p, j); !errors.Is(err, ErrIPUncertain) {
		t.Fatal(err)
	}
	api.loseCreate = false
	if _, err := w.CreateAddress(context.Background(), p, j); err == nil || api.createdAddresses != 1 {
		t.Fatal("unknown existing object adopted", err)
	}
	w, api, p, j = ipFixture(t)
	effect, err := w.CreateAddress(context.Background(), p, j)
	if err != nil {
		t.Fatal(err)
	}
	api.loseDelete = true
	if err := w.RemoveAddress(context.Background(), p, removingAddress(effect.Record)); !errors.Is(err, ErrIPUncertain) {
		t.Fatal(err)
	}
	api.loseDelete = false
	if err := w.RemoveAddress(context.Background(), p, removingAddress(effect.Record)); err != nil || api.deletedAddresses != 1 {
		t.Fatal("explicit absence check retried mutation", err)
	}
	w, api, p, j = ipFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	api.afterCreate = cancel
	if _, err := w.CreateAddress(ctx, p, j); !errors.Is(err, ErrIPUncertain) {
		t.Fatal("late result applied", err)
	}
	if api.createdAddresses != 1 {
		t.Fatal("fixture did not exercise post-write cancellation")
	}
}

func TestWindowsIPPlanRejectsEndpointRecursionAndNonHostRoute(t *testing.T) {
	m, _ := engineMap(t)
	m.Peers[0].Endpoint = "100.64.0.2:51820"
	if _, err := ipPlan(m, policySession, 12345, 7, 8); !errors.Is(err, ErrIPConflict) {
		t.Fatal("endpoint recurses into tunnel", err)
	}
	m, _ = engineMap(t)
	m.Peers[0].AllowedIPs = []string{"0.0.0.0/0"}
	if _, err := ipPlan(m, policySession, 12345, 7, 8); !errors.Is(err, ErrConfiguration) {
		t.Fatal("default route accepted", err)
	}
}
