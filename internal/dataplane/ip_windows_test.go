//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type mockIPAPI struct {
	weakSend                                                         bool
	deviceMissing                                                    bool
	deviceErr                                                        error
	gone                                                             bool
	key                                                              windows.GUID
	addrs                                                            []winipcfg.MibUnicastIPAddressRow
	forward                                                          []winipcfg.MibIPforwardRow2
	createdAddresses, createdRoutes, deletedAddresses, deletedRoutes int
	loseCreate, loseDelete                                           bool
	tentativeOnCreate                                                bool
	afterCreate                                                      func()
}

func (m *mockIPAPI) setWeakSend(_ adapterIdentity, enable bool) (bool, error) {
	changed := m.weakSend != enable
	m.weakSend = enable
	return changed, nil
}

func (m *mockIPAPI) devicePresent(ctx context.Context, _ windows.GUID, _ int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return !m.gone && !m.deviceMissing, m.deviceErr
}

func (m *mockIPAPI) bestInterface(addr netip.Addr) (uint32, error) {
	for _, row := range m.forward {
		if row.DestinationPrefix.Prefix().Contains(addr) {
			return row.InterfaceIndex, nil
		}
	}
	return 0, windows.ERROR_NOT_FOUND
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
	if m.tentativeOnCreate {
		r.DadState = winipcfg.DadStateTentative
	}
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
	api.tentativeOnCreate = true
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

type directRouteTestIP struct {
	*mockIPAPI
	candidate windows.GUID
	wrongBest bool
}

func TestWindowsSessionFailedDirectRouteRestoresPolicyAndPrimary(t *testing.T) {
	s, _, _, m, api, _, _, _, clock := wiredSessionFixture(t)
	sample, _ := clock.Sample()
	fixture := candidateFixtureAt(t, transport.WireGuard, sample.Wall)
	m = nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		plan := *fixture.snapshot.Peers[0].Transport
		localAd := *fixture.snapshot.Node.Transport
		localAd.IntentGeneration = s.generation
		snapshot.Node.Transport = &localAd
		snapshot.Node.EndpointExpiresAt = fixture.snapshot.Node.EndpointExpiresAt
		plan.LocalIntentGeneration = s.generation
		plan.LocalProbeIPv4, plan.PeerProbeIPv4 = "172.20.4.10", "172.20.4.11"
		plan.LocalPublicKey, plan.PeerPublicKey = snapshot.Node.PublicKey, snapshot.Peers[0].PublicKey
		plan.Revision = snapshot.Revision
		snapshot.Peers[0].Transport = &plan
	})
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	assignment, err := newCandidateAssignment(m, s.lease, "peer", fixture.snapshot.Peers[0].Transport.Profiles[0].Profile.ID, sample.Wall)
	if err != nil {
		t.Fatal(err)
	}
	candidateGUID, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	host := &directRouteTestIP{mockIPAPI: api, candidate: candidateGUID, wrongBest: true}
	s.ip.api = host
	api.afterCreate = nil
	candidate := adapterIdentity{LUID: 54321, GUID: candidateGUID, Index: 18}
	identity := s.identity
	s.identity = func(luid uint64) (adapterIdentity, error) {
		if luid == candidate.LUID {
			return candidate, nil
		}
		return identity(luid)
	}
	readFilters := func() []state.FilterRecord {
		t.Helper()
		p, err := s.profile(t.Context(), m, true)
		if err != nil {
			t.Fatal(err)
		}
		clear(p.Keys.WireGuardPrivate)
		return p.Connection.Journal.Filters
	}
	beforeFilters := readFilters()
	beforeRoutes := append([]winipcfg.MibIPforwardRow2(nil), api.forward...)
	beforeCreates := api.createdRoutes
	peer := netip.MustParseAddr("100.70.0.2")
	entry, err := s.stageDirectIP(t.Context(), assignment, candidate, nil)
	if entry != nil || !errors.Is(err, ErrIPConflict) || api.createdRoutes != beforeCreates+1 {
		t.Fatalf("preferred-route verification was not exercised: %v", err)
	}
	if !reflect.DeepEqual(beforeRoutes, api.forward) || len(s.ip.direct) != 0 || api.weakSend || !s.lease.Permissions().NewFlows {
		t.Fatal("failed selection affected the primary route or lease")
	}
	if !reflect.DeepEqual(beforeFilters, readFilters()) {
		t.Fatal("failed selection changed applied policy receipts")
	}
	host.wrongBest = false
	called := false
	entry, err = s.stageDirectIP(t.Context(), assignment, candidate, func() error {
		called = true
		if s.ip.direct[peer] == nil || reflect.DeepEqual(readFilters(), beforeFilters) {
			t.Fatal("commit ran before the preferred route and policy were staged")
		}
		return ErrNoAuthority
	})
	if !called || entry != nil || !errors.Is(err, ErrNoAuthority) || len(s.ip.direct) != 0 ||
		!reflect.DeepEqual(beforeRoutes, api.forward) || !reflect.DeepEqual(beforeFilters, readFilters()) {
		t.Fatal("late activation rejection left native effects", err)
	}
	first, err := s.stageDirectIP(t.Context(), assignment, candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.retireDirectIP(t.Context(), peer, first); err != nil || !reflect.DeepEqual(readFilters(), beforeFilters) || !reflect.DeepEqual(beforeRoutes, api.forward) {
		t.Fatal("retirement did not restore primary policy and route", err)
	}
	second, err := s.stageDirectIP(t.Context(), assignment, candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	activeFilters := readFilters()
	probeLocal, probeRemote, err := assignment.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	checkProbePolicy := func(want wf.Action) {
		t.Helper()
		fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: candidate.LUID, wf.FieldIPLocalAddress: probeLocal,
			wf.FieldIPRemoteAddress: probeRemote, wf.FieldIPProtocol: wf.IPProto(17), wf.FieldIPLocalPort: uint16(0), wf.FieldIPRemotePort: uint16(0)}
		for _, layer := range []wf.LayerID{wf.LayerALEAuthConnectV4, wf.LayerALEAuthRecvAcceptV4} {
			if policyVerdict(s.policy, layer, fields) != want {
				t.Fatal("unexpected maintenance probe decision", layer)
			}
			for _, field := range []wf.FieldID{wf.FieldIPLocalPort, wf.FieldIPRemotePort} {
				fields[field] = uint16(443)
				if policyVerdict(s.policy, layer, fields) != wf.ActionBlock {
					t.Fatal("application port inherited a probe exception", field)
				}
				fields[field] = uint16(0)
			}
		}
	}
	checkProbePolicy(wf.ActionPermit)
	if err := s.retireDirectIP(t.Context(), peer, first); err != ErrIPOwnership || !reflect.DeepEqual(readFilters(), activeFilters) || s.ip.direct[peer] != second {
		t.Fatal("old teardown changed replacement policy or route", err)
	}
	renew := func(changeProbe bool) {
		t.Helper()
		m = nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
			plan := snapshot.Peers[0].Transport
			plan.Revision = snapshot.Revision
			if changeProbe {
				plan.PeerProbeIPv4 = "172.20.4.12"
			}
		})
		if err := s.Refresh(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	renew(false)
	if !reflect.DeepEqual(readFilters(), activeFilters) || s.ip.direct[peer] != second {
		t.Fatal("unrelated signed revision interrupted the selected path")
	}
	renew(true)
	if s.ip.direct[peer] != nil || !reflect.DeepEqual(beforeRoutes, api.forward) || !reflect.DeepEqual(readFilters(), beforeFilters) {
		t.Fatal("changed probe assignment retained old Direct on the same application /32")
	}
	if _, err := s.stageDirectIP(t.Context(), assignment, candidate, nil); err != ErrNoAuthority {
		t.Fatal("previous signed assignment could reselect its route", err)
	}
	assignment, err = newCandidateAssignment(m, s.lease, "peer", assignment.offer.Profile.ID, sample.Wall)
	if err != nil {
		t.Fatal(err)
	}
	offline, err := s.stageDirectIP(t.Context(), assignment, candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	probeLocal, probeRemote, err = assignment.controlAddresses()
	if err != nil {
		t.Fatal(err)
	}
	checkProbePolicy(wf.ActionPermit)
	if err := s.Offline(t.Context()); err != nil || s.ip.direct[peer] != offline {
		t.Fatal("offline map lost an established route", err)
	}
	checkProbePolicy(wf.ActionBlock)
	fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: candidate.LUID, wf.FieldIPLocalAddress: netip.MustParseAddr("100.70.0.1"), wf.FieldIPRemoteAddress: peer, wf.FieldIPProtocol: wf.IPProto(6), wf.FieldIPLocalPort: uint16(50000), wf.FieldIPRemotePort: uint16(443), wf.FieldFlags: wf.ConditionFlag(0)}
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
		t.Fatal("offline Direct permitted a new flow")
	}
	fields[wf.FieldFlags] = wf.ConditionFlagIsReauthorize
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit {
		t.Fatal("offline Direct discarded an established flow")
	}
	renew(false)
	m = nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		p := &snapshot.Peers[0]
		p.Transport.Revision = snapshot.Revision
		p.ACLGrants[0].AllowedPorts[0].Port = 8080
	})
	if err := s.Refresh(t.Context(), m); err != nil || s.ip.direct[peer] != nil {
		t.Fatal("changed grant retained a formerly confirmed route", err)
	}
	fields[wf.FieldIPLocalInterface] = s.policy.luid
	fields[wf.FieldFlags] = wf.ConditionFlag(0)
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
		t.Fatal("old grant survived Direct withdrawal")
	}
	fields[wf.FieldIPRemotePort] = uint16(8080)
	if policyVerdict(s.policy, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit {
		t.Fatal("primary path lost the updated grant")
	}
	assignment, err = newCandidateAssignment(m, s.lease, "peer", assignment.offer.Profile.ID, sample.Wall)
	if err != nil {
		t.Fatal(err)
	}
	beforeCreates = api.createdRoutes
	api.afterCreate = s.lease.Revoke
	entry, err = s.stageDirectIP(t.Context(), assignment, candidate, nil)
	if entry != nil || !errors.Is(err, ErrNoAuthority) || api.createdRoutes != beforeCreates+1 {
		t.Fatal("revocation during native creation did not cancel selection", err)
	}
	if !reflect.DeepEqual(beforeRoutes, api.forward) || len(s.ip.direct) != 0 {
		t.Fatal("revocation retained the candidate route")
	}
	if len(s.policy.DenyAllTraffic().rules) != len(s.policy.rules) || len(s.policy.rules) == 0 {
		t.Fatal("rollback restored application grants after revocation")
	}
}

func (m *directRouteTestIP) bestInterface(addr netip.Addr) (uint32, error) {
	if m.wrongBest || !m.weakSend {
		return 17, nil // primary Wintun wins
	}
	for _, r := range m.forward {
		if r.InterfaceLUID == 54321 && r.DestinationPrefix.Prefix() == netip.PrefixFrom(addr, 32) {
			return r.InterfaceIndex, nil
		}
	}
	return m.mockIPAPI.bestInterface(addr)
}

func (m *directRouteTestIP) createRoute(row *winipcfg.MibIPforwardRow2) error {
	err := m.mockIPAPI.createRoute(row)
	if row.InterfaceLUID == 54321 {
		m.forward[len(m.forward)-1].InterfaceIndex = 18
	}
	return err
}

func (m *directRouteTestIP) guid(luid winipcfg.LUID) (*windows.GUID, error) {
	if luid == 54321 {
		return &m.candidate, nil
	}
	return m.mockIPAPI.guid(luid)
}

func TestWindowsIPDirectRouteOwnershipAcrossRefresh(t *testing.T) {
	w, api, p, j := ipFixture(t)
	// This fixture starts at the route installation step of stageDirectIP.
	api.weakSend = true
	candidate, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	host := &directRouteTestIP{mockIPAPI: api, candidate: candidate}
	w.api = host
	primary, err := w.CreateRoute(t.Context(), p, j.Routes[0], j)
	if err != nil {
		t.Fatal(err)
	}
	j.Routes[0] = primary
	peer := netip.MustParsePrefix(primary.Destination).Addr()
	entry, err := w.installDirectRoute(t.Context(), p, j, candidate, 54321, peer)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(want error) {
		t.Helper()
		if err := w.Preflight(t.Context(), p, j); !errors.Is(err, want) {
			t.Fatalf("preflight: got %v, want %v", err, want)
		}
	}
	refresh(nil)
	if err := w.confirmFilterCleanup(t.Context(), FilterPlan{session: p.adapter, luid: uint64(p.luid)}); err != ErrPolicyCleanupRequired {
		t.Fatal("primary policy could be released with a live candidate", err)
	}
	removing := primary
	removing.Phase = state.EffectRemoving
	if err := w.RemoveRoute(t.Context(), p, removing); err != ErrIPConflict {
		t.Fatal("removed primary route while Direct was active", err)
	}
	if err := w.RemoveAddress(t.Context(), p, removingAddress(j.Addresses[0])); err != ErrIPConflict {
		t.Fatal("removed primary address while Direct was active", err)
	}
	j.Generation++
	refresh(ErrIPOwnership)
	j.Generation--
	host.candidate = windows.GUID{}
	refresh(ErrIPOwnership)
	host.candidate = candidate
	row := api.forward[1]
	api.forward[1].Metric++
	refresh(ErrIPOwnership)
	if err := w.retireDirectRoute(t.Context(), peer, entry); err != ErrIPOwnership || api.deletedRoutes != 0 {
		t.Fatal("modified row deleted", err)
	}
	api.forward = api.forward[:1]
	refresh(ErrIPOwnership)
	api.forward = append(api.forward, row, testIPRoute(777, primary.Destination, 0))
	refresh(ErrIPConflict)
	api.forward = api.forward[:2]
	if err := w.retireDirectRoute(t.Context(), peer, entry); err != nil {
		t.Fatal(err)
	}
	refresh(nil)
	replacement, err := w.installDirectRoute(t.Context(), p, j, candidate, 54321, peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.retireDirectRoute(t.Context(), peer, entry); err != ErrIPOwnership {
		t.Fatal("old receipt removed replacement", err)
	}
	applied := FilterPlan{session: p.adapter, luid: uint64(p.luid), direct: map[uint64]directPeerFilters{54321: {adapter: candidate, peer: peer}}}
	if err := w.retireUnconfirmedDirectRoutes(t.Context(), applied, j); err != nil || w.direct[peer] != replacement {
		t.Fatal("refresh retired a desired Direct route", err)
	}
	withdrawn := applied
	withdrawn.direct = nil
	wrongGeneration := j
	wrongGeneration.Generation++
	if err := w.retireUnconfirmedDirectRoutes(t.Context(), withdrawn, wrongGeneration); err != ErrIPOwnership || w.direct[peer] != replacement {
		t.Fatal("stale refresh retired a replacement", err)
	}
	if err := w.retireUnconfirmedDirectRoutes(t.Context(), withdrawn, j); err != nil || w.direct[peer] != nil {
		t.Fatal("refresh retained withdrawn Direct route", err)
	}
	if err := w.retireUnconfirmedDirectRoutes(t.Context(), withdrawn, j); err != nil {
		t.Fatal(err)
	}
	host.wrongBest = true
	if pending, err := w.installDirectRoute(t.Context(), p, j, candidate, 54321, peer); err != ErrIPConflict || pending != nil || len(w.direct) != 0 {
		t.Fatal("nonpreferred candidate not rolled back", err)
	}
	if len(api.forward) != 1 || !routeMatches(p, api.forward[0], primary) {
		t.Fatal("lost the primary route")
	}
}

func TestWindowsExitIPWithdrawalKeepsPhysicalDefaultAndAssignedSubnet(t *testing.T) {
	ctx := t.Context()
	w, native, _, journal := ipFixture(t)
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0).UTC()
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Node.ID, m.Network.CIDR = "recipient", "100.64.0.0/24"
	peer := &m.Peers[0]
	peer.ID, peer.Endpoint, peer.EndpointExpiresAt = "router", "203.0.113.8:51820", &end
	host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
	app := func(id, prefix string) coordinator.Application {
		return coordinator.Application{ID: id, TargetType: "cidr", Target: prefix, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Connectors: []coordinator.ServiceHost{host}, Routes: []coordinator.ApplicationRoute{{Connector: host, Metric: 1, CIDRs: []string{prefix}, ExpiresAt: end}}}
	}
	m.Network.Applications = []coordinator.Application{app("office", "10.72.0.0/24"), app("exit", "0.0.0.0/0")}
	p, err := ipPlan(m, policySession, 12345, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	_, journal.Routes = p.Prepared()
	physical := testIPRoute(9, "0.0.0.0/0", 11)
	native.forward = []winipcfg.MibIPforwardRow2{physical, testIPRoute(8, "8.8.8.8/32", 1)}
	if err := w.Preflight(ctx, p, journal); !errors.Is(err, ErrIPConflict) || native.createdRoutes != 0 {
		t.Fatal("public route conflict", err)
	}
	native.forward = native.forward[:1]
	for i, record := range journal.Routes {
		applied, err := w.CreateRoute(ctx, p, record, journal)
		if err != nil || applied.Fingerprint == "" {
			t.Fatal(record.Destination, err)
		}
		journal.Routes[i] = applied
	}
	for address, want := range map[string]bool{"8.8.8.8": true, "10.72.0.3": true, "172.20.0.7": false, "192.168.0.8": false} {
		got := slices.ContainsFunc(native.forward, func(row winipcfg.MibIPforwardRow2) bool {
			return row.InterfaceLUID == p.luid && row.DestinationPrefix.Prefix().Contains(netip.MustParseAddr(address))
		})
		if got != want {
			t.Fatal(address, got)
		}
	}
	m.Network.Applications = m.Network.Applications[:1]
	next, err := ipPlan(m, policySession, 12345, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	_, keep := next.Prepared()
	for _, r := range journal.Routes {
		if slices.ContainsFunc(keep, func(k state.RouteRecord) bool { return k.Destination == r.Destination }) {
			continue
		}
		r.Phase = state.EffectRemoving
		if err := w.RemoveRoute(ctx, next, r); err != nil {
			t.Fatal(err)
		}
	}
	if len(native.forward) != len(keep)+1 || !reflect.DeepEqual(native.forward[0], physical) {
		t.Fatal("withdrawal changed the physical default", native.forward)
	}
	for _, r := range keep {
		if _, err := native.route(p.luid, netip.MustParsePrefix(r.Destination), netip.IPv4Unspecified()); err != nil {
			t.Fatal(r.Destination, err)
		}
	}
}

func TestWindowsManagedSubnetRouteConflictReceiptAndCleanup(t *testing.T) {
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID = "source"
	m.Peers[0].ID = "router"
	m.Peers[0].EndpointExpiresAt = &end
	router := coordinator.ServiceHost{NodeID: "router", PublicKey: m.Peers[0].PublicKey}
	const target = "10.27.4.0/24"
	app := coordinator.Application{ID: "printer", TargetType: "cidr", Target: target, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Connectors: []coordinator.ServiceHost{router}, Routes: []coordinator.ApplicationRoute{{Connector: router, Metric: 1, ExpiresAt: end, CIDRs: []string{target}}}}
	m.Network.Applications = []coordinator.Application{app}
	w, api, _, j := ipFixture(t)
	p, err := ipPlan(m, policySession, 12345, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	a, routes := p.Prepared()
	j.Addresses, j.Routes = []state.AddressRecord{a}, routes
	if len(routes) != 2 || routes[0].Destination != target {
		t.Fatal(routes)
	}
	for _, foreign := range []string{target, "10.27.4.128/25", "10.27.4.11/32", "10.27.0.0/16"} {
		api.forward = []winipcfg.MibIPforwardRow2{testIPRoute(9, foreign, 1)}
		if _, err := w.CreateRoute(t.Context(), p, routes[0], j); !errors.Is(err, ErrIPConflict) || api.createdRoutes != 0 {
			t.Fatal(foreign, err)
		}
	}
	foreignLocal := p.address
	foreignLocal.InterfaceLUID = 9
	if err := foreignLocal.Address.SetAddr(netip.MustParseAddr("10.27.4.61")); err != nil {
		t.Fatal(err)
	}
	api.addrs = []winipcfg.MibUnicastIPAddressRow{foreignLocal}
	api.forward = nil
	if err := w.Preflight(t.Context(), p, j); !errors.Is(err, ErrIPConflict) {
		t.Fatal("foreign address", err)
	}
	api.addrs = nil
	base := testIPRoute(9, "0.0.0.0/0", 1)
	api.forward = []winipcfg.MibIPforwardRow2{base}
	record, err := w.CreateRoute(t.Context(), p, routes[0], j)
	if err != nil || record.Destination != target || record.Fingerprint == "" {
		t.Fatal(record, err)
	}
	j.Routes[0] = record
	if err := w.inspectRoute(t.Context(), p, record); err != nil {
		t.Fatal(err)
	}
	cleanup, err := PrepareIPCleanup(policySession, 12345, a.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	record.Phase = state.EffectRemoving
	api.forward[1].Metric++
	if err := w.RemoveRoute(t.Context(), cleanup, record); !errors.Is(err, ErrIPOwnership) {
		t.Fatal("replaced route", err)
	}
	api.forward[1].Metric--
	if err := w.RemoveRoute(t.Context(), cleanup, record); err != nil {
		t.Fatal(err)
	}
	if len(api.forward) != 1 || !reflect.DeepEqual(api.forward[0], base) {
		t.Fatal("unrelated route changed")
	}
	m.Peers[0].Endpoint = "10.27.4.55:51820"
	if _, err := ipPlan(m, policySession, 12345, 7, 8); !errors.Is(err, ErrIPConflict) {
		t.Fatal("recursive endpoint", err)
	}
}
