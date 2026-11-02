//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"testing"
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
