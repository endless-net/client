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
	"unsafe"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

const policySession = "703a8ce6-a672-4f87-af40-56bb8c599720"

func policyFixture(t *testing.T, offline bool) FilterPlan {
	t.Helper()
	m, _ := engineMap(t)
	m.Peers[0].ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}, {Protocol: "udp", Port: 53}}}}
	m.Peers[0].IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.1/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}}}
	p, err := filterPlan(m, policySession, 12345, 17, 8, 8, offline)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Evaluates only our sublayer's scalar conditions, not the Windows firewall.
// Native BFE classification/reauthorization remains a separate privileged check.
func policyVerdict(p FilterPlan, layer wf.LayerID, fields map[wf.FieldID]any) wf.Action {
	var chosen wf.Action
	var weight uint64
	for _, r := range p.rules {
		if r.Layer != layer {
			continue
		}
		match := true
		for _, c := range r.Conditions {
			value, exists := fields[c.Field]
			if !exists {
				match = false
				break
			}
			if c.Op == wf.MatchTypeFlagsAllSet {
				if value.(wf.ConditionFlag)&c.Value.(wf.ConditionFlag) != c.Value.(wf.ConditionFlag) {
					match = false
					break
				}
			} else if !reflect.DeepEqual(value, c.Value) {
				match = false
				break
			}
		}
		if match && r.Weight > weight {
			chosen, weight = r.Action, r.Weight
		}
	}
	return chosen
}

func TestWindowsPolicyDirectionPortContextAndOffline(t *testing.T) {
	for _, offline := range []bool{false, true} {
		p := policyFixture(t, offline)
		for _, tc := range []struct {
			name                  string
			layer                 wf.LayerID
			protocol              wf.IPProto
			localPort, remotePort uint16
			reauth                bool
			luid                  uint64
			remote                string
			want                  wf.Action
		}{
			{"outbound443", wf.LayerALEAuthConnectV4, 6, 50000, 443, false, 12345, "100.64.0.2", wf.ActionPermit},
			{"reply reauth stays at connect layer", wf.LayerALEAuthConnectV4, 6, 50000, 443, true, 12345, "100.64.0.2", wf.ActionPermit},
			{"ingress22", wf.LayerALEAuthRecvAcceptV4, 6, 22, 50000, false, 12345, "100.64.0.2", wf.ActionPermit},
			{"ingress reauth", wf.LayerALEAuthRecvAcceptV4, 6, 22, 50000, true, 12345, "100.64.0.2", wf.ActionPermit},
			{"udp53", wf.LayerALEAuthConnectV4, 17, 50000, 53, true, 12345, "100.64.0.2", wf.ActionPermit},
			{"wrong protocol", wf.LayerALEAuthConnectV4, 17, 50000, 443, true, 12345, "100.64.0.2", wf.ActionBlock},
			{"wrong port", wf.LayerALEAuthConnectV4, 6, 50000, 80, true, 12345, "100.64.0.2", wf.ActionBlock},
			{"no reverse grant", wf.LayerALEAuthRecvAcceptV4, 6, 443, 50000, true, 12345, "100.64.0.2", wf.ActionBlock},
			{"foreign peer", wf.LayerALEAuthConnectV4, 6, 50000, 443, true, 12345, "100.64.0.3", wf.ActionBlock},
			{"foreign interface untouched", wf.LayerALEAuthConnectV4, 6, 50000, 443, true, 98765, "100.64.0.2", 0},
		} {
			want := tc.want
			if offline && !tc.reauth && want == wf.ActionPermit {
				want = wf.ActionBlock
			}
			fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: tc.luid, wf.FieldIPLocalAddress: netip.MustParseAddr("100.64.0.1"), wf.FieldIPRemoteAddress: netip.MustParseAddr(tc.remote), wf.FieldIPProtocol: tc.protocol, wf.FieldIPLocalPort: tc.localPort, wf.FieldIPRemotePort: tc.remotePort, wf.FieldFlags: wf.ConditionFlag(0)}
			if tc.reauth {
				fields[wf.FieldFlags] = wf.ConditionFlagIsReauthorize
			}
			if got := policyVerdict(p, tc.layer, fields); got != want {
				t.Fatalf("%s offline%t got%v want%v", tc.name, offline, got, want)
			}
		}
		for _, r := range p.rules {
			if r.Action == wf.ActionPermit && r.HardAction {
				t.Fatal("permit bypasses other firewall blocks")
			}
		}
	}
}

func TestWindowsTimedGrantsKeepIndependentPermitsOnPrimaryAndDirect(t *testing.T) {
	m, _ := engineMap(t)
	end := time.Unix(1800000000, 0)
	later := end.Add(time.Second)
	peer := &m.Peers[0]
	permit := func(port int, until *time.Time) coordinator.ACLGrant {
		return coordinator.ACLGrant{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: port}}, ValidUntil: until}
	}
	peer.ACLGrants = []coordinator.ACLGrant{permit(443, &end), permit(443, &later), permit(22, nil), permit(22, &end)}
	local, remote := netip.MustParseAddr(m.Node.AssignedIP), netip.MustParseAddr("100.64.0.2")
	for _, offline := range []bool{false, true} {
		base, err := filterPlan(m, policySession, 12345, 17, 8, 8, offline)
		if err != nil {
			t.Fatal(err)
		}
		installed, err := base.withDirectPeer(local, remote, 12346, 18)
		if err != nil {
			t.Fatal(err)
		}
		for _, at := range []time.Time{end, later} {
			plan, err := base.currentGrants(at).withDirectPeer(local, remote, 12346, 18)
			if err != nil {
				t.Fatal(err)
			}
			for _, luid := range []uint64{12345, 12346} {
				fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: luid, wf.FieldIPLocalAddress: local, wf.FieldIPRemoteAddress: remote, wf.FieldIPProtocol: wf.IPProto(6), wf.FieldFlags: wf.ConditionFlagIsReauthorize}
				for _, port := range []uint16{443, 22} {
					fields[wf.FieldIPRemotePort] = port
					want := port == 22 || at.Before(later)
					for _, candidate := range []FilterPlan{plan, installed.currentGrants(at)} {
						permitted := policyVerdict(candidate, wf.LayerALEAuthConnectV4, fields) == wf.ActionPermit
						if permitted != want {
							t.Fatalf("offline=%t interface=%d port=%d time=%v permit=%t", offline, luid, port, at, permitted)
						}
					}
				}
			}
		}
	}
}

func TestWindowsPolicyStableIDsAndPreparedJournal(t *testing.T) {
	p := policyFixture(t, false)
	records := p.Records()
	for _, r := range records {
		id, err := uuid.Parse(r.Key)
		if err != nil || id.String() != r.Key || r.ID != 0 || r.Phase != state.EffectPrepared || len(r.Fingerprint) != 64 {
			t.Fatal("invalid prepared proof")
		}
	}
	records[0].Fingerprint = "changed"
	if p.Records()[0].Fingerprint == "changed" {
		t.Fatal("mutable plan exposed")
	}
	m, _ := engineMap(t)
	m.Peers[0].ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.2/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 53}, {Protocol: "tcp", Port: 443}}}}
	m.Peers[0].IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"100.64.0.1/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 22}}}}
	reordered, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
	if err != nil || !reflect.DeepEqual(p.Records(), reordered.Records()) {
		t.Fatal("reordering churned owned filters", err)
	}
	m.Peers = nil
	denied, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
	if err != nil || len(denied.rules) != 6 {
		t.Fatal("empty policy was not deny-all", err)
	}
	m, _ = engineMap(t)
	m.Peers[0].ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{"0.0.0.0/0"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}}}
	if _, err := filterPlan(m, policySession, 12345, 17, 8, 8, false); !errors.Is(err, ErrConfiguration) {
		t.Fatal("foreign target accepted")
	}
}

func TestWindowsAllProtocolsPermitOnlySignedDirectionAndAddress(t *testing.T) {
	m, _ := engineMap(t)
	peer := &m.Peers[0]
	peer.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}}}
	plan, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, offline := range []bool{false, true} {
		nativePlan, err := filterPlan(m, policySession, 12345, 17, 8, 8, offline)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range nativePlan.rules {
			var arena filterArena
			_, err := marshalNativeFilter(&arena, r)
			arena.close()
			if err != nil {
				t.Fatalf("native rule for offline=%v: %v", offline, err)
			}
		}
	}
	fields := map[wf.FieldID]any{
		wf.FieldIPLocalInterface: uint64(12345), wf.FieldIPLocalAddress: netip.MustParseAddr("100.64.0.1"),
		wf.FieldIPRemoteAddress: netip.MustParseAddr("100.64.0.2"), wf.FieldIPProtocol: wf.IPProto(1),
		wf.FieldFlags: wf.ConditionFlag(0),
	}
	if policyVerdict(plan, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit ||
		policyVerdict(plan, wf.LayerALEAuthRecvAcceptV4, fields) != wf.ActionBlock {
		t.Fatal("ICMP permit lost its direction")
	}
	fields[wf.FieldIPRemoteAddress] = netip.MustParseAddr("100.64.0.3")
	if policyVerdict(plan, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
		t.Fatal("unrelated peer matched")
	}
}

func TestWindowsMarshalSignedAllProtocols(t *testing.T) {
	m, _ := engineMap(t)
	peer := &m.Peers[0]
	all := []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}
	peer.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: all}}
	peer.IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{m.Node.AssignedIP + "/32"}, AllowedPorts: all}}
	for _, offline := range []bool{false, true} {
		plan, err := filterPlan(m, policySession, 12345, 17, 8, 8, offline)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range plan.rules {
			var arena filterArena
			_, err := marshalNativeFilter(&arena, rule)
			arena.close()
			if err != nil {
				t.Fatalf("marshal layer %v, offline %v: %v", rule.Layer, offline, err)
			}
			if rule.Action != wf.ActionPermit || (rule.Layer != wf.LayerALEAuthConnectV4 && rule.Layer != wf.LayerALEAuthRecvAcceptV4) {
				continue
			}
			for _, field := range []*wf.Match{
				{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: wf.IPProto(6)},
				{Field: wf.FieldIPLocalPort, Op: wf.MatchTypeEqual, Value: uint16(443)},
				{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeEqual, Value: uint16(443)},
			} {
				partial := *rule
				partial.Conditions = append(append([]*wf.Match(nil), rule.Conditions...), field)
				if supportedNativeRule(&partial) {
					t.Fatal("accepted a partial transport restriction", field.Field)
				}
			}
		}
	}
}

type mockFilterAPI struct {
	persistent, layerLive, layerPending    bool
	layerOwner, dynamicRef                 *mockFilterAPI
	live, pending                          map[wf.RuleID]uint64
	next                                   uint64
	fail                                   string
	commits, aborts, removes, adds, closes int
	onCommit                               func()
	abortFailure                           bool
}

func (m *mockFilterAPI) begin() error {
	if m.fail == "begin" {
		return ErrPolicy
	}
	m.pending = make(map[wf.RuleID]uint64)
	m.layerPending = m.layerLive
	for k, v := range m.live {
		m.pending[k] = v
	}
	return nil
}
func (m *mockFilterAPI) commit() error {
	m.commits++
	m.live = m.pending
	m.layerLive = m.layerPending
	m.pending = nil
	if m.onCommit != nil {
		m.onCommit()
	}
	if m.fail == "commit" {
		return errors.New("lost BFE response")
	}
	return nil
}
func (m *mockFilterAPI) abort() error {
	m.aborts++
	m.pending = nil
	if m.fail == "abort" || m.abortFailure {
		return ErrPolicy
	}
	return nil
}
func (m *mockFilterAPI) sublayer(windows.GUID) error {
	if m.fail == "sublayer" {
		return ErrPolicy
	}
	m.layerPending = true
	return nil
}
func (m *mockFilterAPI) checkSublayer(windows.GUID) error {
	if m.fail == "sublayer-check" {
		return ErrPolicyOwnership
	}
	live := m.layerLive
	if m.pending != nil {
		live = m.layerPending
	}
	if m.layerOwner != nil {
		live = m.layerOwner.layerLive
	}
	if !live {
		return windows.Errno(windows.FWP_E_SUBLAYER_NOT_FOUND)
	}
	return nil
}
func (m *mockFilterAPI) removeSublayer(windows.GUID) error {
	if len(m.pending) != 0 {
		return ErrPolicy
	}
	if m.dynamicRef != nil && len(m.dynamicRef.live) != 0 {
		return ErrPolicy
	}
	if m.fail == "remove-sublayer" {
		return ErrPolicy
	}
	m.layerPending = false
	return nil
}
func (m *mockFilterAPI) add(r *wf.Rule) (uint64, error) {
	m.adds++
	if m.fail == "add" {
		return 0, ErrPolicy
	}
	m.next++
	m.pending[r.ID] = m.next
	return m.next, nil
}
func (m *mockFilterAPI) check(r *wf.Rule) (uint64, error) {
	if m.fail == "check" {
		return 0, ErrPolicyOwnership
	}
	rows := m.live
	if m.pending != nil {
		rows = m.pending
	}
	id := rows[r.ID]
	if id == 0 {
		return 0, windows.Errno(windows.FWP_E_FILTER_NOT_FOUND)
	}
	return id, nil
}
func (m *mockFilterAPI) remove(id wf.RuleID) error {
	m.removes++
	if m.fail == "remove" {
		return ErrPolicy
	}
	delete(m.pending, id)
	return nil
}
func (m *mockFilterAPI) close() error {
	m.closes++
	if m.fail == "close" {
		return ErrPolicy
	}
	if !m.persistent {
		m.live = nil
	}
	m.pending = nil
	return nil
}

func filterController(t *testing.T) (*WindowsFilters, *mockFilterAPI) {
	t.Helper()
	key, err := canonicalGUID(policySession)
	if err != nil {
		t.Fatal(err)
	}
	api := &mockFilterAPI{next: 2000}
	gapi := &mockFilterAPI{persistent: true, next: 1000}
	api.layerOwner = gapi
	gapi.dynamicRef = api
	return &WindowsFilters{api: api, session: key, guards: &persistentGuards{api: gapi, key: key}, writeGuardIDs: func(context.Context, []state.FilterRecord) error { return nil }}, api
}

func TestWindowsFilterAtomicRefreshAndOwnership(t *testing.T) {
	f, api := filterController(t)
	online, offline := policyFixture(t, false), policyFixture(t, true)
	records, err := f.Replace(context.Background(), online)
	if err != nil || api.commits != 1 || len(records) != len(online.rules) {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.ID == 0 || r.Phase != state.EffectApplied {
			t.Fatal("missing applied result")
		}
	}
	adds := api.adds
	if _, err = f.Replace(context.Background(), online); err != nil || api.adds != adds || api.removes != 0 {
		t.Fatal("same policy churned", err)
	}
	if _, err = f.Replace(context.Background(), offline); err != nil || len(api.live)+len(f.guards.ids) != len(offline.rules) {
		t.Fatal("offline transition failed", err)
	}
	for _, r := range offline.rules {
		if r.Persistent {
			continue
		}
		if api.live[r.ID] == 0 {
			t.Fatal("partial policy committed")
		}
	}
	// Same GUID replaced by a different OS object is not ours to remove.
	var changed wf.RuleID
	for _, r := range offline.rules {
		if !r.Persistent {
			changed = r.ID
			break
		}
	}
	api.live[changed]++
	removes := api.removes
	if _, err = f.Replace(context.Background(), online); !errors.Is(err, ErrPolicyOwnership) || api.removes != removes || api.aborts != 1 {
		t.Fatal("foreign object deleted", err)
	}
	if err := f.Close(); !errors.Is(err, ErrPolicyCleanupRequired) {
		t.Fatal("guards closed before native cleanup", err)
	}
	ip := &WindowsIP{api: &mockIPAPI{gone: true}, maxRows: 128}
	if err := f.CloseAfterIPCleanup(context.Background(), ip); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil || api.closes != 1 {
		t.Fatal("close not idempotent")
	}
}

func TestWindowsFilterFailuresNeverReportApplied(t *testing.T) {
	for _, failure := range []string{"begin", "sublayer-check", "add", "check", "commit"} {
		t.Run(failure, func(t *testing.T) {
			f, api := filterController(t)
			api.fail = failure
			records, err := f.Replace(context.Background(), policyFixture(t, false))
			if err == nil || len(records) != 0 {
				t.Fatal("failed update claimed applied")
			}
			if failure == "commit" && !errors.Is(err, ErrPolicyUncertain) {
				t.Fatal("lost commit called rollback", err)
			}
			if failure != "commit" && len(api.live) != 0 {
				t.Fatal("partial transaction exposed")
			}
			if _, err := f.Replace(context.Background(), policyFixture(t, false)); !errors.Is(err, ErrPolicy) {
				t.Fatal("failed session silently retried")
			}
		})
	}
	f, api := filterController(t)
	ctx, cancel := context.WithCancel(context.Background())
	api.onCommit = cancel
	if _, err := f.Replace(ctx, policyFixture(t, false)); !errors.Is(err, ErrPolicyUncertain) || len(api.live) == 0 {
		t.Fatal("late commit treated as cancelled rollback", err)
	}
}

func TestWindowsFilterUpdateAbortAndCloseUncertainty(t *testing.T) {
	for _, abortFailure := range []bool{false, true} {
		f, api := filterController(t)
		if _, err := f.Replace(context.Background(), policyFixture(t, false)); err != nil {
			t.Fatal(err)
		}
		before := make(map[wf.RuleID]uint64, len(api.live))
		for k, v := range api.live {
			before[k] = v
		}
		api.fail, api.abortFailure = "remove", abortFailure
		_, err := f.Replace(context.Background(), policyFixture(t, true))
		want := ErrPolicy
		if abortFailure {
			want = ErrPolicyUncertain
		}
		if !errors.Is(err, want) || !reflect.DeepEqual(api.live, before) || api.commits != 1 || api.aborts != 1 {
			t.Fatal("failed replacement claimed a new policy", err)
		}
		api.fail = "close"
		ip := &WindowsIP{api: &mockIPAPI{gone: true}, maxRows: 128}
		if err := f.CloseAfterIPCleanup(context.Background(), ip); !errors.Is(err, ErrPolicyUncertain) || f.closed {
			t.Fatal("failed close claimed cleanup", err)
		}
		api.fail = ""
		if err := f.CloseAfterIPCleanup(context.Background(), ip); err != nil || !f.closed {
			t.Fatal("explicit close could not finish", err)
		}
	}
}

func TestWindowsFilterMarshallerRejectsUnsupportedEffects(t *testing.T) {
	p := policyFixture(t, false)
	for _, change := range []func(*wf.Rule){
		func(r *wf.Rule) { r.Persistent = !r.Persistent },
		func(r *wf.Rule) { r.BootTime = true },
		func(r *wf.Rule) { r.PermitIfMissing = true },
		func(r *wf.Rule) { r.Layer = wf.LayerALEAuthConnectV6 },
		func(r *wf.Rule) { r.Weight = 999 },
		func(r *wf.Rule) { r.Action = wf.ActionCalloutUnknown },
		func(r *wf.Rule) { r.ProviderData = []byte{1} },
	} {
		r := *p.rules[0]
		change(&r)
		var arena filterArena
		_, err := marshalNativeFilter(&arena, &r)
		arena.close()
		if !errors.Is(err, ErrConfiguration) {
			t.Fatal("unsupported native effect silently dropped", err)
		}
	}
	if classifyPolicyError(windows.ERROR_ACCESS_DENIED) != ErrPolicyPermission || classifyPolicyError(windows.Errno(windows.FWP_E_FILTER_NOT_FOUND)) != ErrPolicyOwnership || classifyPolicyError(errors.New("raw BFE transport error")) != ErrPolicy {
		t.Fatal("native errors misclassified")
	}
}

func TestWindowsFilterSDKLayoutAndOwnershipComparison(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("005 native ABI is Windows amd64")
	}
	if unsafe.Sizeof(nativeFilter{}) != 200 || unsafe.Offsetof(nativeFilter{}.Context) != 152 || unsafe.Offsetof(nativeFilter{}.Reserved) != 168 || unsafe.Offsetof(nativeFilter{}.ID) != 176 || unsafe.Sizeof(nativeCondition{}) != 40 || unsafe.Sizeof(nativeSession{}) != 72 || unsafe.Sizeof(nativeSublayer{}) != 72 {
		t.Fatal("Win64 SDK layout mismatch")
	}
	p := policyFixture(t, true)
	var arena filterArena
	defer arena.close()
	rule := p.rules[0]
	for _, r := range p.rules {
		if r.Action == wf.ActionPermit {
			rule = r
			break
		}
	}
	a, err := marshalNativeFilter(&arena, rule)
	if err != nil {
		t.Fatal(err)
	}
	b, err := marshalNativeFilter(&arena, rule)
	if err != nil {
		t.Fatal(err)
	}
	if !sameNativeFilter(a, b) {
		t.Fatal("same native configuration differs")
	}
	a.Flags |= 0x40
	if !sameNativeFilter(a, b) {
		t.Fatal("automatic BFE index changed ownership")
	}
	a.Flags |= 0x20
	if sameNativeFilter(a, b) {
		t.Fatal("accepted a disabled filter")
	}
	a.Flags &^= 0x20
	conditions := unsafe.Slice(a.Conditions, int(a.Count))
	slices.Reverse(conditions)
	if !sameNativeFilter(a, b) {
		t.Fatal("OS condition reordering broke ownership proof")
	}
	conditions[0].Match = uint32(wf.MatchTypeNotEqual)
	if sameNativeFilter(a, b) {
		t.Fatal("changed native condition accepted")
	}
}

func TestPolicyDiagnosticKeepsTypedWindowsError(t *testing.T) {
	native := windows.Errno(windows.FWP_E_IN_USE)
	err := policyErrorWithWindowsCode(ErrPolicy, errors.Join(native, errors.New("private transport details")))
	var code windows.Errno
	if !errors.Is(err, ErrPolicy) || !errors.As(err, &code) || code != native ||
		strings.Contains(err.Error(), "private transport details") {
		t.Fatal("policy diagnostic lost its safe code", err)
	}
}
func TestWindowsPolicyKeepsPeerAndPortTogetherAcrossReplacement(t *testing.T) {
	m, _ := engineMap(t)
	makePeer := func(address string, port int) coordinator.Peer {
		prefix := address + "/32"
		return coordinator.Peer{AllowedIPs: []string{prefix}, ACLRestricted: true, ACLGrants: []coordinator.ACLGrant{
			{DestinationCIDRs: []string{prefix}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: port}}},
		}}
	}
	const b, c = "100.64.0.2", "100.64.0.3"
	m.Peers = []coordinator.Peer{makePeer(b, 443), makePeer(c, 22)}
	for _, removed := range []bool{false, true} {
		if removed {
			m.Peers[0].ACLGrants = nil
		}
		plan, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
		if err != nil {
			t.Fatal(err)
		}
		if !removed {
			local := netip.MustParseAddr(m.Node.AssignedIP)
			first, second := netip.MustParseAddr(b), netip.MustParseAddr(c)
			one, err := plan.withDirectPeer(local, first, 54321, 18)
			if err != nil {
				t.Fatal(err)
			}
			both, err := one.withDirectPeer(local, second, 54322, 19)
			if err != nil {
				t.Fatal(err)
			}
			remaining, err := both.withoutDirectPeer(first, 54321)
			if err != nil {
				t.Fatal(err)
			}
			want, err := plan.withDirectPeer(local, second, 54322, 19)
			if err != nil || !reflect.DeepEqual(remaining.Records(), want.Records()) {
				t.Fatal("retirement changed rules of the other active peer", err)
			}
			if _, err := both.withoutDirectPeer(second, 54321); err != ErrPolicyOwnership {
				t.Fatal("accepted another peer's receipt", err)
			}
		}
		for _, remote := range []string{b, c} {
			for _, port := range []uint16{22, 443} {
				for _, layer := range []wf.LayerID{wf.LayerALEAuthConnectV4, wf.LayerALEAuthRecvAcceptV4} {
					localPort, remotePort := uint16(50000), port
					if layer == wf.LayerALEAuthRecvAcceptV4 {
						localPort, remotePort = remotePort, localPort
					}
					fields := map[wf.FieldID]any{
						wf.FieldIPLocalInterface: uint64(12345), wf.FieldIPLocalAddress: netip.MustParseAddr(m.Node.AssignedIP),
						wf.FieldIPRemoteAddress: netip.MustParseAddr(remote), wf.FieldIPProtocol: wf.IPProto(6),
						wf.FieldIPLocalPort: localPort, wf.FieldIPRemotePort: remotePort,
					}
					want := wf.ActionBlock
					if layer == wf.LayerALEAuthConnectV4 && ((remote == b && port == 443 && !removed) || (remote == c && port == 22)) {
						want = wf.ActionPermit
					}
					if got := policyVerdict(plan, layer, fields); got != want {
						t.Fatalf("removed=%t remote=%s port=%d layer=%v got=%v want=%v", removed, remote, port, layer, got, want)
					}
				}
			}
		}
	}
}

func TestWindowsDirectPeerFilterPlanPreservesPairAuthority(t *testing.T) {
	local := netip.MustParseAddr("100.64.0.1")
	peer := netip.MustParseAddr("100.64.0.2")
	other := netip.MustParseAddr("100.64.0.3")
	for _, offline := range []bool{false, true} {
		base := policyFixture(t, offline)
		const candidateLUID, candidateIndex = uint64(23456), uint32(18)
		active, err := base.withDirectPeer(local, peer, candidateLUID, candidateIndex)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range active.rules {
			if !supportedNativeRule(r) {
				t.Fatal("cannot encode Direct policy rule", r.Layer)
			}
		}
		fields := map[wf.FieldID]any{wf.FieldIPLocalInterface: candidateLUID, wf.FieldInterfaceIndex: candidateIndex,
			wf.FieldIPLocalAddress: local, wf.FieldIPRemoteAddress: peer, wf.FieldIPProtocol: wf.IPProto(6),
			wf.FieldIPRemotePort: uint16(443), wf.FieldIPLocalPort: uint16(57000), wf.FieldFlags: wf.ConditionFlag(0)}
		want := wf.ActionPermit
		if offline {
			want = wf.ActionBlock
		}
		if got := policyVerdict(active, wf.LayerALEAuthConnectV4, fields); got != want {
			t.Fatal("unexpected Direct TCP decision", got, want)
		}
		fields[wf.FieldFlags] = wf.ConditionFlagIsReauthorize
		if policyVerdict(active, wf.LayerALEAuthConnectV4, fields) != wf.ActionPermit || policyVerdict(active, wf.LayerALEAuthRecvAcceptV4, fields) != wf.ActionBlock {
			t.Fatal("directions of signed grant changed")
		}
		fields[wf.FieldIPProtocol] = wf.IPProto(17)
		if policyVerdict(active, wf.LayerALEAuthConnectV4, fields) != wf.ActionBlock {
			t.Fatal("wrong protocol acquired a grant")
		}
		for _, owner := range []uint64{base.luid, candidateLUID} {
			fields[wf.FieldIPLocalInterface] = owner
			for _, layer := range []wf.LayerID{wf.LayerOutboundIPPacketV4, wf.LayerInboundIPPacketV4} {
				fields[wf.FieldIPRemoteAddress] = peer
				if policyVerdict(active, layer, fields) != wf.ActionPermit || policyVerdict(base, layer, fields) != wf.ActionBlock {
					t.Fatal("missing or leaked pair exception")
				}
				fields[wf.FieldIPRemoteAddress] = other
				if policyVerdict(active, layer, fields) != wf.ActionBlock {
					t.Fatal("other peer inherited the active route")
				}
			}
		}
		if _, err := base.DenyAllTraffic().withDirectPeer(local, peer, candidateLUID, candidateIndex); err != ErrNoAuthority {
			t.Fatal("denied plan generated a route exception", err)
		}
	}
}
