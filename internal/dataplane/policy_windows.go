//go:build windows

package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var ErrPolicy = errors.New("packet policy failed")
var ErrPolicyOwnership = errors.New("packet policy ownership changed")
var ErrPolicyUncertain = errors.New("packet policy outcome uncertain")
var ErrPolicyPermission = errors.New("packet policy permission denied")
var ErrPolicyLimit = errors.New("packet policy scope limit exceeded")
var ErrPolicyCleanupRequired = errors.New("native cleanup not confirmed")

func canonicalGUID(value string) (windows.GUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return windows.GUID{}, ErrConfiguration
	}
	key, err := windows.GUIDFromString("{" + value + "}")
	if err != nil {
		return windows.GUID{}, ErrConfiguration
	}
	return key, nil
}

// FilterPlan contains only native effects, no key or credential. Prepare its
// Records in the durable journal before calling WindowsFilters.Replace.
type FilterPlan struct {
	session      windows.GUID
	luid         uint64
	index        uint32
	maxProtected int
	scopes       map[ipGuardScope]bool
	rules        []*wf.Rule
}

type ipGuardScope struct {
	field   wf.FieldID
	address netip.Addr
}

func (FilterPlan) String() string   { return "[Windows filter plan]" }
func (FilterPlan) GoString() string { return "[Windows filter plan]" }

func PrepareFilterPlan(ctx context.Context, m connectivity.Map, sessionID string, luid uint64, index uint32, maxGrants, maxProtected int, offline bool) (FilterPlan, error) {
	if ctx == nil {
		return FilterPlan{}, ErrConfiguration
	}
	if ctx.Err() != nil {
		return FilterPlan{}, ctx.Err()
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		return FilterPlan{}, ErrConfiguration
	}
	if m.Denied() {
		snapshot.Peers = nil
	}
	key, err := canonicalGUID(sessionID)
	if err != nil || luid == 0 || index == 0 {
		return FilterPlan{}, ErrConfiguration
	}
	row, err := winipcfg.LUID(luid).Interface()
	if err != nil {
		return FilterPlan{}, classifyPolicyError(err)
	}
	if row == nil || row.InterfaceGUID != key || row.InterfaceLUID != winipcfg.LUID(luid) || row.InterfaceIndex != index {
		return FilterPlan{}, ErrPolicyOwnership
	}
	if ctx.Err() != nil {
		return FilterPlan{}, ctx.Err()
	}
	return filterPlan(snapshot, sessionID, luid, index, maxGrants, maxProtected, offline)
}

func filterPlan(m coordinator.NetworkMapSnapshot, sessionID string, luid uint64, index uint32, maxGrants, maxProtected int, offline bool) (FilterPlan, error) {
	session, err := uuid.Parse(sessionID)
	if err != nil || session.String() != sessionID || session == uuid.Nil || luid == 0 || index == 0 || maxGrants <= 0 || maxProtected <= 0 {
		return FilterPlan{}, ErrConfiguration
	}
	guid, err := windows.GUIDFromString("{" + sessionID + "}")
	if err != nil {
		return FilterPlan{}, ErrConfiguration
	}
	local, err := netip.ParseAddr(m.Node.AssignedIP)
	if err != nil || !local.Is4() || local.IsUnspecified() || local.IsLoopback() || local.IsMulticast() {
		return FilterPlan{}, ErrConfiguration
	}
	plan := FilterPlan{session: guid, luid: luid, index: index, maxProtected: maxProtected, scopes: make(map[ipGuardScope]bool)}
	if err := plan.protect(wf.FieldIPLocalAddress, local); err != nil {
		return FilterPlan{}, err
	}
	equal := func(field wf.FieldID, value any) *wf.Match {
		return &wf.Match{Field: field, Op: wf.MatchTypeEqual, Value: value}
	}
	add := func(layer wf.LayerID, action wf.Action, extra ...*wf.Match) {
		r := &wf.Rule{Name: "EndlessNet device policy", Layer: layer, Sublayer: wf.SublayerID(guid), Weight: 1, Action: action, HardAction: action == wf.ActionBlock,
			Conditions: append([]*wf.Match{equal(wf.FieldIPLocalInterface, luid)}, extra...)}
		if action == wf.ActionPermit {
			r.Weight = 2
		}
		plan.addRule(r)
	}
	add(wf.LayerALEAuthConnectV4, wf.ActionBlock)
	add(wf.LayerALEAuthRecvAcceptV4, wf.ActionBlock)
	count := 0
	seenIP := map[netip.Addr]bool{local: true}
	for _, peer := range m.Peers {
		if len(peer.AllowedIPs) != 1 || !peer.ACLRestricted || len(peer.AllowedPorts) != 0 {
			return FilterPlan{}, ErrConfiguration
		}
		remote, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || !remote.Addr().Is4() || remote.Bits() != 32 || remote.String() != peer.AllowedIPs[0] || seenIP[remote.Addr()] {
			return FilterPlan{}, ErrConfiguration
		}
		seenIP[remote.Addr()] = true
		if err := plan.protect(wf.FieldIPRemoteAddress, remote.Addr()); err != nil {
			return FilterPlan{}, err
		}
		for _, direction := range []struct {
			grants      []coordinator.ACLGrant
			layer       wf.LayerID
			destination string
			portField   wf.FieldID
		}{
			{peer.ACLGrants, wf.LayerALEAuthConnectV4, remote.String(), wf.FieldIPRemotePort},
			{peer.IngressACLGrants, wf.LayerALEAuthRecvAcceptV4, local.String() + "/32", wf.FieldIPLocalPort},
		} {
			for _, grant := range direction.grants {
				if len(grant.DestinationCIDRs) != 1 || grant.DestinationCIDRs[0] != direction.destination || len(grant.AllowedPorts) == 0 {
					return FilterPlan{}, ErrConfiguration
				}
				for _, port := range grant.AllowedPorts {
					count++
					if count > maxGrants || port.Port < 1 || port.Port > 65535 || port.Protocol != "tcp" && port.Protocol != "udp" {
						return FilterPlan{}, ErrConfiguration
					}
					protocol := wf.IPProto(6)
					if port.Protocol == "udp" {
						protocol = 17
					}
					conditions := []*wf.Match{equal(wf.FieldIPLocalAddress, local), equal(wf.FieldIPRemoteAddress, remote.Addr()), equal(wf.FieldIPProtocol, protocol), equal(direction.portField, uint16(port.Port))}
					if offline {
						conditions = append(conditions, &wf.Match{Field: wf.FieldFlags, Op: wf.MatchTypeFlagsAllSet, Value: wf.ConditionFlagIsReauthorize})
					}
					add(direction.layer, wf.ActionPermit, conditions...)
				}
			}
		}
	}
	// Duplicate identical grants produce one native effect. Ordering is stable
	// across producer peer/grant ordering, so refresh doesn't churn all filters.
	plan.canonicalize()
	return plan, nil
}

func (p *FilterPlan) addRule(r *wf.Rule) {
	r.Name = "EndlessNet device policy"
	r.Sublayer = wf.SublayerID(p.session)
	session, _ := uuid.Parse(strings.Trim(p.session.String(), "{}"))
	raw, _ := json.Marshal(r)
	hash := sha256.Sum256(append(session[:], raw...))
	id := uuid.UUID(hash[:16])
	id[6] = id[6]&0x0f | 0x80
	id[8] = id[8]&0x3f | 0x80
	key, _ := windows.GUIDFromString("{" + id.String() + "}")
	r.ID = wf.RuleID(key)
	p.rules = append(p.rules, r)
}

func (p *FilterPlan) protect(field wf.FieldID, address netip.Addr) error {
	scope := ipGuardScope{field: field, address: address}
	if p.scopes[scope] {
		return nil
	}
	if !hostIPv4(address) || field != wf.FieldIPLocalAddress && field != wf.FieldIPRemoteAddress {
		return ErrConfiguration
	}
	if len(p.scopes) >= p.maxProtected {
		return ErrPolicyLimit
	}
	p.scopes[scope] = true
	// Packet layers evaluate each packet independently of ALE flow caching.
	// Scope blocks survive a route/interface change; only the owned actual
	// interface index AND source/destination-local LUID get a soft exception.
	for _, layer := range []wf.LayerID{wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		p.addRule(&wf.Rule{Layer: layer, Action: wf.ActionBlock, HardAction: true, Persistent: true, Weight: 1, Conditions: []*wf.Match{{Field: field, Op: wf.MatchTypeEqual, Value: address}}})
		p.addRule(&wf.Rule{Layer: layer, Action: wf.ActionPermit, Weight: 2, Conditions: []*wf.Match{{Field: field, Op: wf.MatchTypeEqual, Value: address}, {Field: wf.FieldIPLocalInterface, Op: wf.MatchTypeEqual, Value: p.luid}, {Field: wf.FieldInterfaceIndex, Op: wf.MatchTypeEqual, Value: p.index}}})
	}
	return nil
}

// RetainIPGuards keeps the IP keys of unfinished effects, including removing
// rows. It grants no application traffic. Releasing these guards is part of
// final native cleanup, not a side effect of a peer disappearing from a map.
func (p FilterPlan) RetainIPGuards(j state.ResourceJournal) (FilterPlan, error) {
	key, err := canonicalGUID(j.ID)
	if err != nil || key != p.session || j.Adapter == nil || j.Adapter.ID != j.ID || j.Adapter.LUID != p.luid {
		return FilterPlan{}, ErrPolicyOwnership
	}
	next := p
	next.rules = append([]*wf.Rule(nil), p.rules...)
	next.scopes = make(map[ipGuardScope]bool, len(p.scopes))
	for scope := range p.scopes {
		next.scopes[scope] = true
	}
	add := func(field wf.FieldID, value string) error {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() != 32 || prefix.String() != value {
			return ErrConfiguration
		}
		return next.protect(field, prefix.Addr())
	}
	for _, a := range j.Addresses {
		if err := add(wf.FieldIPLocalAddress, a.Prefix); err != nil {
			return FilterPlan{}, err
		}
	}
	for _, r := range j.Routes {
		if r.NextHop != "0.0.0.0" {
			return FilterPlan{}, ErrConfiguration
		}
		if err := add(wf.FieldIPRemoteAddress, r.Destination); err != nil {
			return FilterPlan{}, err
		}
	}
	next.canonicalize()
	return next, nil
}

// DenyAllTraffic removes packet exceptions too, so interface-index reuse during
// teardown cannot turn an old tunnel exception into a foreign-interface permit.
func (p FilterPlan) DenyAllTraffic() FilterPlan {
	next := p
	next.rules = make([]*wf.Rule, 0, len(p.rules))
	for _, r := range p.rules {
		if r.Action == wf.ActionPermit {
			continue
		}
		next.rules = append(next.rules, r)
	}
	return next
}

func (p *FilterPlan) canonicalize() {
	sort.Slice(p.rules, func(i, j int) bool { return p.rules[i].ID.String() < p.rules[j].ID.String() })
	unique := p.rules[:0]
	for _, r := range p.rules {
		if len(unique) == 0 || unique[len(unique)-1].ID != r.ID {
			unique = append(unique, r)
		}
	}
	p.rules = unique
}

func (p FilterPlan) Records() []state.FilterRecord {
	result := make([]state.FilterRecord, len(p.rules))
	for i, rule := range p.rules {
		raw, _ := json.Marshal(rule)
		hash := sha256.Sum256(raw)
		result[i] = state.FilterRecord{Key: strings.ToLower(strings.Trim(rule.ID.String(), "{}")), EffectStamp: state.EffectStamp{Phase: state.EffectPrepared, Fingerprint: hex.EncodeToString(hash[:])}}
		if rule.Persistent {
			c := rule.Conditions[0]
			result[i].GuardAddress = c.Value.(netip.Addr).String()
			result[i].GuardLocal = c.Field == wf.FieldIPLocalAddress
			result[i].GuardInbound = rule.Layer == wf.LayerInboundIPPacketV4
		}
	}
	return result
}

func ipv4Number(ip netip.Addr) uint32 { value := ip.As4(); return binary.BigEndian.Uint32(value[:]) }
