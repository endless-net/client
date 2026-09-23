package client

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/device"
)

func TestResourceHostRouteRequiresActualOwnedLookupAndAssignedSource(t *testing.T) {
	target := netip.MustParseAddr("100.64.0.2")
	own := underlayDNSInterface{Index: 7, Name: "endlessnet", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	sources := []netip.Prefix{netip.MustParsePrefix("100.64.0.1/32")}
	valid := `[{"dst":"100.64.0.2","dev":"endlessnet","prefsrc":"100.64.0.1","flags":[],"cache":[],"uid":0}]`
	for _, scenario := range []string{"valid", "foreign", "absent", "gateway", "wrong_source", "encap", "duplicate", "flags"} {
		t.Run(scenario, func(t *testing.T) {
			raw := valid
			switch scenario {
			case "foreign":
				raw = strings.ReplaceAll(raw, "endlessnet", "eth0")
			case "absent":
				raw = `[]`
			case "gateway":
				raw = strings.Replace(raw, `"uid":0`, `"gateway":"100.64.0.3"`, 1)
			case "wrong_source":
				raw = strings.ReplaceAll(raw, "100.64.0.1", "100.64.0.3")
			case "encap":
				raw = strings.Replace(raw, `"uid":0`, `"encap":{}`, 1)
			case "duplicate":
				raw = strings.Replace(raw, `"dev":"endlessnet"`, `"dev":"endlessnet","dev":"eth0"`, 1)
			case "flags":
				raw = strings.Replace(raw, `"flags":[]`, `"flags":["linkdown"]`, 1)
			}
			err := resourceHostRouteObserved([]byte(raw), target, own, sources)
			if (err == nil) != (scenario == "valid") {
				t.Fatal("incorrect route proof", scenario, err)
			}
		})
	}
	if err := resourceHostRouteObserved([]byte(valid), target, own, nil); err == nil {
		t.Fatal("unassigned planned source accepted")
	}
}

func TestResourceHostRouteCommandIsUnforcedAndCancellable(t *testing.T) {
	own := underlayDNSInterface{Index: 7, Name: "endlessnet", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("fd00::1")}}
	sources := []netip.Prefix{netip.MustParsePrefix("fd00::1/128")}
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		want := []string{"-j", "-N", "-6", "route", "get", "fd00::2", "mark", "0"}
		if name != "ip" || !reflect.DeepEqual(args, want) {
			t.Fatal("forced or incorrect route lookup", name, args)
		}
		return []byte(`[{"dst":"fd00::2","dev":"endlessnet","prefsrc":"fd00::1","flags":[]}]`), nil
	}
	if err := observeResourceHostRoute(t.Context(), netip.MustParseAddr("fd00::2"), own, sources, runner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := observeResourceHostRoute(ctx, netip.MustParseAddr("fd00::2"), own, sources, func(ctx context.Context, _ string, _ ...string) ([]byte, error) { return nil, ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestResourceHostPathRequiresFreshAuthenticatedDirectEvidence(t *testing.T) {
	now := time.Now().UTC()
	peer := WireGuardPeerInspection{Endpoint: "192.0.2.1:51820", LatestHandshakeUnix: now.Unix(), latestHandshakeNanos: int64(now.Nanosecond()), handshakeTimeComplete: true}
	path := PeerPathStatus{PeerID: "peer", SelectedPath: "direct", LastTransitionAt: now.Add(-time.Second).Format(time.RFC3339Nano), SelectedEndpoint: peer.Endpoint, Direct: PathCandidateStatus{Endpoint: peer.Endpoint, State: "reachable", CheckedAt: now.Format(time.RFC3339Nano)}}
	if !resourceHostPathObserved([]PeerPathStatus{path}, "peer", peer, now) {
		t.Fatal("fresh direct path rejected")
	}
	for _, scenario := range []string{"old_handshake", "future_handshake", "old_probe", "endpoint", "relay", "missing_transition", "handshake_before_transition", "incomplete_handshake"} {
		t.Run(scenario, func(t *testing.T) {
			p, s := peer, path
			switch scenario {
			case "old_handshake":
				p.LatestHandshakeUnix = now.Add(-device.RejectAfterTime).Unix()
			case "future_handshake":
				p.LatestHandshakeUnix = now.Add(time.Minute).Unix()
			case "old_probe":
				s.Direct.CheckedAt = now.Add(-device.RejectAfterTime).Format(time.RFC3339Nano)
			case "endpoint":
				s.SelectedEndpoint = "192.0.2.2:51820"
			case "relay":
				s.SelectedPath = "relay"
				s.Relay.State = "reachable"
			case "missing_transition":
				s.LastTransitionAt = ""
			case "handshake_before_transition":
				s.LastTransitionAt = now.Add(time.Nanosecond).Format(time.RFC3339Nano)
			case "incomplete_handshake":
				p.handshakeTimeComplete = false
			}
			if resourceHostPathObserved([]PeerPathStatus{s}, "peer", p, now) {
				t.Fatal("unsupported or stale path accepted")
			}
		})
	}
}

// Projection tests may use a synthetic immutable proof attached to an actual
// configured in-memory engine. This fixture does not qualify native route or
// handshake evidence; those boundaries have separate injected-parser tests.
func resourceHostProjectionFixture(t *testing.T) (Config, *WireGuardEngine, *ResourceHostObservation) {
	t.Helper()
	n, cfg, _ := nativeExitResumeFixture(t)
	// Apply the persisted representation also used by the service/store fixture.
	cfg = clonePersistentConfig(cfg)
	// Keep this deterministic parser/projection fixture free of background probes.
	n.engine.mu.Lock()
	n.engine.pathCancel = func() {}
	n.engine.mu.Unlock()
	if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)
	proof := &ResourceHostObservation{engine: e, device: e.device, configuration: resourceObservationConfig(cfg), uapi: sha256.Sum256([]byte(e.uapi)), paths: resourceObservationPaths(e), pathManager: e.relayPaths, expires: time.Now().Add(time.Minute), hosts: map[string]bool{id: true}}
	proof.lifetime = exitLANTestLifetime(t)
	return cfg, e, proof
}

// Parser/path units inject a stable stream, never native networking. Dedicated
// topology tests cover changes and lifetime ownership through publication.
func observeResourceHostsTest(t *testing.T, e *WireGuardEngine, ctx context.Context, cfg Config, runner CommandRunner, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) (*ResourceHostObservation, error) {
	t.Helper()
	proof, err := e.observeResourceHostsWithTopology(ctx, cfg, runner, now, inspect, func(context.Context) (exitLANChangeStream, error) {
		return &exitLANTestStream{changed: make(chan struct{})}, nil
	})
	if proof != nil {
		t.Cleanup(func() { _ = proof.Close() })
	}
	return proof, err
}

func TestResourceHostProofRejectsChangedBindingAndExpiredRuntime(t *testing.T) {
	cfg, e, proof := resourceHostProjectionFixture(t)
	if !proof.Current(cfg, time.Now()) {
		t.Fatal("fixture not current")
	}
	changed := clonePersistentConfig(cfg)
	changed.LocalOwnerID = "different"
	if proof.Current(changed, time.Now()) {
		t.Fatal("foreign owner retained evidence")
	}
	changed = clonePersistentConfig(cfg)
	changed.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
	if proof.Current(changed, time.Now()) {
		t.Fatal("disconnected intent retained evidence")
	}
	if proof.Current(cfg, proof.expires) {
		t.Fatal("expired evidence retained")
	}
	e.mu.Lock()
	if proof.Current(cfg, time.Now()) {
		t.Fatal("busy engine reported evidence")
	}
	e.mu.Unlock()
	e.mu.Lock()
	e.runtimeSuspended = true
	e.mu.Unlock()
	if proof.Current(cfg, time.Now()) {
		t.Fatal("suspended runtime retained evidence")
	}
	e.mu.Lock()
	e.runtimeSuspended = false
	e.mu.Unlock()
}

func TestResourceHostInterfaceRejectsDownOrReplacedIdentity(t *testing.T) {
	for _, flags := range []string{`["UP"]`, `[]`, `["UP","LOOPBACK"]`} {
		runner := func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`[{"ifindex":7,"ifname":"endlessnet","flags":` + flags + `,"addr_info":[{"local":"100.64.0.1"}]}]`), nil
		}
		_, err := resourceRouteInterface(t.Context(), "endlessnet", runner)
		if (err == nil) != (flags == `["UP"]`) {
			t.Fatal("invalid native interface accepted", err)
		}
	}
}

func TestResourceHostRulesRejectFlowSpecificRouting(t *testing.T) {
	plain := `[{"priority":0,"src":"all","table":"255","protocol":"2"},{"priority":32766,"src":"all","table":"254","protocol":"2"}]`
	if !resourceHostRulesObserved([]byte(plain), 0) {
		t.Fatal("plain routing rejected")
	}
	for _, selector := range []string{`"uidrange":"1000-2000"`, `"ipproto":"tcp"`, `"sport":"443"`, `"dport":"443"`, `"iif":"lo"`, `"oif":"eth0"`, `"tos":"0x10"`, `"goto":100`, `"l3mdev":true`} {
		raw := strings.Replace(plain, `"priority":32766`, `"priority":32766,`+selector, 1)
		if resourceHostRulesObserved([]byte(raw), 51820) {
			t.Fatal("flow-dependent routing accepted", selector)
		}
	}
	if resourceHostRulesObserved([]byte(strings.Replace(plain, `"src":"all"`, `"src":"192.0.2.0/24"`, 1)), 0) {
		t.Fatal("source policy accepted")
	}
	owned := `[{"priority":100,"src":"all","not":null,"fwmark":"0xca6c","fwmask":"0xffffffff","table":"254","protocol":"0","suppress_prefixlen":0},{"priority":101,"src":"all","not":null,"fwmark":"0xca6c","table":"51820","protocol":"0"}]`
	if !resourceHostRulesObserved([]byte(owned), 51820) {
		t.Fatal("owned exit rules rejected")
	}
	if resourceHostRulesObserved([]byte(owned), 51999) {
		t.Fatal("foreign mark scope accepted")
	}
}

func TestResourceHostIPv6UsesPreferredSourceNotFromSelector(t *testing.T) {
	own := underlayDNSInterface{Index: 7, Name: "endlessnet", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("fd00::1")}}
	raw := []byte(`[{"dst":"fd00::2","from":"::","dev":"endlessnet","prefsrc":"fd00::1","metric":1024,"pref":"medium","flags":[],"mark":"0x0"}]`)
	if err := resourceHostRouteObserved(raw, netip.MustParseAddr("fd00::2"), own, []netip.Prefix{netip.MustParsePrefix("fd00::1/128")}); err != nil {
		t.Fatal(err)
	}
	missing := strings.Replace(string(raw), `,"prefsrc":"fd00::1"`, "", 1)
	if resourceHostRouteObserved([]byte(missing), netip.MustParseAddr("fd00::2"), own, []netip.Prefix{netip.MustParsePrefix("fd00::1/128")}) == nil {
		t.Fatal("source selector substituted for selected source")
	}
}

func TestResourceHostCollectorBindsNativeRulesRoutesAndPeerPath(t *testing.T) {
	for _, scenario := range []string{"confirmed", "missing_route", "uid_policy", "replaced_interface", "wrong_owner"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, _ := resourceHostProjectionFixture(t)
			now := time.Now().UTC()
			e.mu.Lock()
			inspection, err := resourceObservedUAPI(e)
			if err != nil {
				e.mu.Unlock()
				t.Fatal(err)
			}
			mapPeer := cfg.CachedMap.Peers[0]
			live, ok := wireGuardPeerForMapPeer(inspection, mapPeer)
			if !ok {
				e.mu.Unlock()
				t.Fatal("missing fixture peer")
			}
			e.relayPaths.statuses = []PeerPathStatus{{PeerID: mapPeer.ID, SelectedPath: "direct", LastTransitionAt: now.Add(-time.Second).Format(time.RFC3339Nano), SelectedEndpoint: live.Endpoint, Direct: PathCandidateStatus{Endpoint: live.Endpoint, State: "reachable", CheckedAt: now.Format(time.RFC3339Nano)}}}
			iface, local := e.interface_, e.routerCfg.Addresses[0].Addr().String()
			e.mu.Unlock()
			if scenario == "wrong_owner" {
				cfg.LocalOwnerID = "foreign-owner"
			}
			addressReads, calls := 0, 0
			runner := func(ctx context.Context, _ string, args ...string) ([]byte, error) {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded native observation")
				}
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "rule show") {
					if scenario == "uid_policy" {
						return []byte(`[{"priority":32766,"src":"all","table":"254","uidrange":"1000-2000"}]`), nil
					}
					return []byte(`[{"priority":32766,"src":"all","table":"254"}]`), nil
				}
				if strings.Contains(joined, "address show") {
					addressReads++
					index := 7
					if scenario == "replaced_interface" && addressReads > 1 {
						index = 8
					}
					return []byte(fmt.Sprintf(`[{"ifindex":%d,"ifname":%q,"flags":["UP"],"addr_info":[{"local":%q}]}]`, index, iface, local)), nil
				}
				if scenario == "missing_route" {
					return []byte(`[]`), nil
				}
				return []byte(fmt.Sprintf(`[{"dst":%q,"dev":%q,"prefsrc":%q,"flags":[]}]`, args[5], iface, local)), nil
			}
			// Stable security-relevant UAPI comes from the real injected device;
			// only handshake time is a deterministic captured observation fixture.
			inspect := func(engine *WireGuardEngine) (WireGuardInspection, error) {
				value, err := resourceObservedUAPI(engine)
				for i := range value.Peers {
					value.Peers[i].LatestHandshakeUnix = now.Unix()
					value.Peers[i].latestHandshakeNanos = int64(now.Nanosecond())
					value.Peers[i].handshakeTimeComplete = true
				}
				return value, err
			}
			proof, err := observeResourceHostsTest(t, e, t.Context(), cfg, runner, now, inspect)
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, mapPeer.ID)
			confirmed := err == nil && proof.HostConfirmed(id) && proof.Current(cfg, time.Now())
			if confirmed != (scenario == "confirmed") {
				t.Fatal("incorrect collector proof", scenario, err)
			}
			if scenario == "wrong_owner" && calls != 0 {
				t.Fatal("foreign owner reached native commands")
			}
		})
	}
}

func TestResourceFlowCollectorBindsSignedServiceSubnetAndLiveRoute(t *testing.T) {
	n, cfg, _ := nativeExitResumeFixture(t)
	n.engine.mu.Lock()
	n.engine.pathCancel = func() {}
	n.engine.mu.Unlock()
	trusted, _, key := signedApplicationFixture(t, false)
	cfg.MapSigningTrust = trusted.MapSigningTrust
	peer := &cfg.CachedMap.Peers[0]
	cfg.CachedMap.Network.Services = []api.AdvertisedService{{ID: "db", Name: "db", DNSName: "db.account.endlessnet", Ports: []api.ServicePort{{Protocol: "tcp", Port: 5432}}, ApprovalMode: "manual", ApprovalStatus: "approved", Hosts: []api.ServiceHost{{NodeID: peer.ID, PublicKey: peer.PublicKey}}}}
	cfg.CachedMap.Network.ClientPolicy.Resources = []api.ManagedResourceSetting{{Kind: api.ManagedResourceSubnet, ID: peer.ID, CIDR: "100.64.0.2/32", Source: api.ClientPolicyAccount, PolicyID: "subnet", Enabled: true}}
	resignApplicationMap(t, cfg.CachedMap, key)
	cfg = clonePersistentConfig(cfg)
	peer = &cfg.CachedMap.Peers[0]
	if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	e := n.engine
	now := time.Now().UTC()
	e.mu.Lock()
	inspection, err := resourceObservedUAPI(e)
	if err != nil {
		e.mu.Unlock()
		t.Fatal(err)
	}
	live, ok := wireGuardPeerForMapPeer(inspection, *peer)
	if !ok {
		e.mu.Unlock()
		t.Fatal("signed service host has no configured peer")
	}
	e.relayPaths.statuses = []PeerPathStatus{{PeerID: peer.ID, SelectedPath: "direct", LastTransitionAt: now.Add(-time.Second).Format(time.RFC3339Nano), SelectedEndpoint: live.Endpoint, Direct: PathCandidateStatus{Endpoint: live.Endpoint, State: "reachable", CheckedAt: now.Format(time.RFC3339Nano)}}}
	iface, localAddress := e.interface_, e.routerCfg.Addresses[0].Addr().String()
	e.mu.Unlock()
	request := resourceTestTCP("100.64.0.1", "100.64.0.2", 40000, 5432, 0x02)
	response := resourceTestTCP("100.64.0.2", "100.64.0.1", 5432, 40000, 0x12)
	e.resourceFlows.observe(request, false, now)
	e.resourceFlows.observe(response, true, now)
	routes := 0
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "rule show") {
			return []byte(`[{"priority":32766,"src":"all","table":"254"}]`), nil
		}
		if strings.Contains(joined, "address show") {
			return []byte(fmt.Sprintf(`[{"ifindex":7,"ifname":%q,"flags":["UP"],"addr_info":[{"local":%q}]}]`, iface, localAddress)), nil
		}
		routes++
		return []byte(fmt.Sprintf(`[{"dst":%q,"dev":%q,"prefsrc":%q,"flags":[]}]`, args[5], iface, localAddress)), nil
	}
	inspect := func(engine *WireGuardEngine) (WireGuardInspection, error) {
		value, err := resourceObservedUAPI(engine)
		for i := range value.Peers {
			value.Peers[i].LatestHandshakeUnix = now.Unix()
			value.Peers[i].latestHandshakeNanos = int64(now.Nanosecond())
			value.Peers[i].handshakeTimeComplete = true
		}
		return value, err
	}
	proof, err := observeResourceHostsTest(t, e, t.Context(), cfg, runner, now, inspect)
	serviceID := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "db\x00tcp\x005432")
	subnetID := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x00100.64.0.2/32")
	if err != nil || !proof.ResourceConfirmed(serviceID) || !proof.ResourceConfirmed(subnetID) || routes < 2 || !proof.Current(cfg, time.Now()) {
		t.Fatal("flow did not survive signed identity, path, route and final readback", err, routes)
	}
	subnetOnly := *proof
	subnetOnly.hosts = nil
	subnetOnly.resources = map[string][]resourceFlowSample{subnetID: proof.resources[subnetID]}
	protected := netip.MustParsePrefix("100.64.0.2/32")
	e.applicationFilter.mu.Lock()
	e.applicationFilter.protected[protected] = true
	e.applicationFilter.mu.Unlock()
	if subnetOnly.Current(cfg, time.Now()) {
		t.Fatal("application reservation widened to subnet availability")
	}
	e.applicationFilter.mu.Lock()
	delete(e.applicationFilter.protected, protected)
	e.applicationFilter.mu.Unlock()
	foreignRoute := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "route get") {
			return []byte(fmt.Sprintf(`[{"dst":%q,"dev":"eth0","prefsrc":%q,"flags":[]}]`, args[5], localAddress)), nil
		}
		return runner(ctx, name, args...)
	}
	foreignProof, err := observeResourceHostsTest(t, e, t.Context(), cfg, foreignRoute, now, inspect)
	if err != nil || foreignProof.ResourceConfirmed(serviceID) || foreignProof.ResourceConfirmed(subnetID) {
		t.Fatal("exit or LAN route overrode resource destination", err)
	}
	e.resourceFlows.reset()
	if proof.Current(cfg, time.Now()) {
		t.Fatal("reapply retained stale resource availability")
	}
	m := newRPCStoreTest(t)
	cfg.RPCState.Revision = 1
	cfg.RPCState.DigestKey = make([]byte, 32)
	cfg.RPCState.Operations = map[string]clientRPCOperationRecord{}
	if err := m.store.Update(func(next *Config) error { *next = clonePersistentConfig(cfg); return nil }); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	s.ResourceObservationLock = &sync.Mutex{}
	s.ResourceEnforcementProvider = e.TryResourceEnforcement
	s.ResourceHostProvider = func(ctx context.Context, current Config) (*ResourceHostObservation, error) {
		return e.observeResourceHostsWithTopology(ctx, current, runner, time.Now(), inspect, func(context.Context) (exitLANChangeStream, error) {
			return &exitLANTestStream{changed: make(chan struct{})}, nil
		})
	}
	owner := local.Peer{Identity: cfg.LocalOwnerID}
	list := &ipc.ListResourcesRequest{Profile: &ipc.ProfileRef{ProfileId: cfg.RPCState.ActiveProfileID}}
	if err := s.publishResourceClock(t.Context()); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	eventsCtx, cancelEvents := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelEvents()
	if _, err := sub.next(eventsCtx); err != nil {
		t.Fatal(err)
	}
	check := func(available bool) {
		t.Helper()
		rows, err := s.resourcesAs(t.Context(), owner, list)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, row := range rows.Resources {
			if row.Id == serviceID || row.Id == subnetID {
				found++
				if (row.Availability.Availability == ipc.Availability_AVAILABILITY_AVAILABLE) != available {
					t.Fatal("resource snapshot ignored live flow evidence", row.Kind, row.Availability)
				}
			}
		}
		if found != 2 {
			t.Fatal("signed service and single-IP subnet not projected", found)
		}
	}
	check(false)
	for _, available := range []bool{true, false} {
		if available {
			e.resourceFlows.observe(request, false, time.Now())
			e.resourceFlows.observe(response, true, time.Now())
		} else {
			e.resourceFlows.reset()
		}
		check(available)
		before := m.Metadata().Revision
		if err := s.publishResourceClock(t.Context()); err != nil || m.Metadata().Revision != before+1 {
			t.Fatal("resource evidence transition did not invalidate revision", err)
		}
		for {
			event, err := sub.next(eventsCtx)
			if err != nil {
				t.Fatal("resource evidence transition did not invalidate events", err)
			}
			if invalidated := event.GetInvalidated(); invalidated != nil {
				if invalidated.Domain != ipc.Domain_DOMAIN_RESOURCES {
					t.Fatal("resource evidence invalidated the wrong domain", event)
				}
				break
			}
		}
	}
}

func TestResourceHostCurrentRejectsWithdrawnOrOverlappingFilters(t *testing.T) {
	for _, scenario := range []string{"acl_withdrawn", "acl_applying", "application_target", "application_source", "sharing_target", "unrelated_application", "inbound_stale"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, proof := resourceHostProjectionFixture(t)
			target := netip.MustParsePrefix(cfg.CachedMap.Peers[0].AllowedIPs[0])
			switch scenario {
			case "acl_withdrawn":
				e.peerACLFilter.withdraw()
			case "acl_applying":
				e.peerACLFilter.mu.Lock()
				e.peerACLFilter.applying = true
				e.peerACLFilter.mu.Unlock()
			case "application_target", "application_source", "unrelated_application":
				prefix := target
				if scenario == "application_source" {
					prefix = netip.PrefixFrom(netip.MustParseAddr(cfg.CachedMap.Node.AssignedIP), 32)
				}
				if scenario == "unrelated_application" {
					prefix = netip.MustParsePrefix("192.0.2.0/24")
				}
				e.applicationFilter.mu.Lock()
				e.applicationFilter.protected[prefix] = true
				e.applicationFilter.grants = nil
				e.applicationFilter.mu.Unlock()
			case "sharing_target":
				e.sharingFilter.mu.Lock()
				e.sharingFilter.protected[target.Addr()] = true
				e.sharingFilter.mu.Unlock()
			case "inbound_stale":
				e.inboundFilter.mu.Lock()
				e.inboundFilter.binding = "different"
				e.inboundFilter.mu.Unlock()
			}
			if proof.Current(cfg, time.Now()) != (scenario == "unrelated_application") {
				t.Fatal("incorrect filter eligibility", scenario)
			}
		})
	}
}

func TestResourceHostPortScopedGrantIsNotWholeHostAvailability(t *testing.T) {
	n, cfg, _ := nativeExitResumeFixture(t)
	n.engine.mu.Lock()
	n.engine.pathCancel = func() {}
	n.engine.mu.Unlock()
	trusted, _, key := signedApplicationFixture(t, false)
	cfg.MapSigningTrust = trusted.MapSigningTrust
	cfg.CachedMap.Peers[0].ACLRestricted = true
	cfg.CachedMap.Peers[0].AllowedPorts = []api.ACLPort{{Protocol: "tcp", Port: 443}}
	resignApplicationMap(t, cfg.CachedMap, key)
	if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	n.engine.mu.Lock()
	defer n.engine.mu.Unlock()
	eligible, err := resourceHostFilterEligibility(n.engine, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if eligible[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)] {
		t.Fatal("port grant became unrestricted host availability")
	}
}

func TestResourceHostACLUnrelatedAndUnrestrictedGrant(t *testing.T) {
	target := netip.MustParseAddr("100.64.0.2")
	rules := []peerACLRule{{destination: netip.MustParsePrefix("192.0.2.0/24"), deny: true}}
	if !resourceHostACLUnrestricted(rules, target) {
		t.Fatal("unrelated restriction blocked host")
	}
	rules = []peerACLRule{{destination: netip.MustParsePrefix("100.64.0.0/24"), deny: true, grants: []peerACLGrant{{destination: netip.MustParsePrefix("100.64.0.2/32")}}}}
	if !resourceHostACLUnrestricted(rules, target) {
		t.Fatal("explicit unrestricted grant rejected")
	}
	rules[0].grants[0].ports = []api.ACLPort{{Protocol: "udp", Port: 53}}
	if resourceHostACLUnrestricted(rules, target) {
		t.Fatal("restricted grant widened")
	}
}
