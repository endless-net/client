//go:build windows

package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
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
var ErrPolicyUncertain = errors.New("packet policy outcome uncertain")
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
	direct       map[uint64]directPeerFilters
	session      windows.GUID
	luid         uint64
	index        uint32
	maxProtected int
	scopes       map[ipGuardScope]bool
	rules        []*wf.Rule
}

type directPeerFilters struct {
	adapter                     windows.GUID
	peerID, profileID           string
	localEndpoint, peerEndpoint string
	peer                        netip.Addr
	index                       uint32
	rules                       map[wf.RuleID]bool
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
					if count > maxGrants || !port.Valid() {
						return FilterPlan{}, ErrConfiguration
					}
					conditions := []*wf.Match{equal(wf.FieldIPLocalAddress, local), equal(wf.FieldIPRemoteAddress, remote.Addr())}
					if port.Protocol != coordinator.ProtocolAll {
						protocol := wf.IPProto(6)
						if port.Protocol == "udp" {
							protocol = 17
						}
						conditions = append(conditions, equal(wf.FieldIPProtocol, protocol), equal(direction.portField, uint16(port.Port)))
					}
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

// withDirectPeer extends this session's current WFP policy to one owned
// WireGuardNT interface. Derive every ALE exception from the source plan so a
// removed grant or offline reauthorization restriction survives the transition.
func (p FilterPlan) withDirectPeer(local, remote netip.Addr, luid uint64, index uint32) (FilterPlan, error) {
	if luid == 0 || luid == p.luid || index == 0 || index == p.index || local == remote ||
		!p.scopes[ipGuardScope{wf.FieldIPLocalAddress, local}] || !p.scopes[ipGuardScope{wf.FieldIPRemoteAddress, remote}] {
		return FilterPlan{}, ErrPolicyOwnership
	}
	equal := func(field wf.FieldID, value any) *wf.Match {
		return &wf.Match{Field: field, Op: wf.MatchTypeEqual, Value: value}
	}
	next := p
	next.rules = append([]*wf.Rule(nil), p.rules...)
	for owner, filters := range p.direct {
		if owner == luid || filters.peer == remote || filters.index == index {
			return FilterPlan{}, ErrPolicyOwnership
		}
	}
	granted := false
	for _, r := range p.rules {
		if r.Layer != wf.LayerALEAuthConnectV4 && r.Layer != wf.LayerALEAuthRecvAcceptV4 {
			continue
		}
		if r.Action != wf.ActionPermit || len(r.Conditions) < 3 ||
			r.Conditions[0].Field != wf.FieldIPLocalInterface || r.Conditions[0].Value != p.luid ||
			r.Conditions[1].Field != wf.FieldIPLocalAddress || r.Conditions[1].Value != local ||
			r.Conditions[2].Field != wf.FieldIPRemoteAddress || r.Conditions[2].Value != remote {
			continue
		}
		copyRule := *r
		copyRule.ID = wf.RuleID{}
		copyRule.Conditions = append([]*wf.Match{equal(wf.FieldIPLocalInterface, luid)}, r.Conditions[1:]...)
		next.addRule(&copyRule)
		granted = true
	}
	if !granted {
		return FilterPlan{}, ErrNoAuthority
	}
	for _, layer := range []wf.LayerID{wf.LayerALEAuthConnectV4, wf.LayerALEAuthRecvAcceptV4} {
		next.addRule(&wf.Rule{Layer: layer, Action: wf.ActionBlock, HardAction: true, Weight: 1,
			Conditions: []*wf.Match{equal(wf.FieldIPLocalInterface, luid)}})
	}
	// In weak-host mode the local address can still belong to Wintun. Both
	// exceptions require the real WireGuardNT interface index and exact pair.
	for _, layer := range []wf.LayerID{wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		for _, owner := range []uint64{p.luid, luid} {
			next.addRule(&wf.Rule{Layer: layer, Action: wf.ActionPermit, Weight: 2,
				Conditions: []*wf.Match{equal(wf.FieldIPLocalAddress, local), equal(wf.FieldIPRemoteAddress, remote),
					equal(wf.FieldIPLocalInterface, owner), equal(wf.FieldInterfaceIndex, index)}})
		}
	}
	ids := make(map[wf.RuleID]bool, len(next.rules)-len(p.rules))
	for _, r := range next.rules[len(p.rules):] {
		ids[r.ID] = true
	}
	next.direct = maps.Clone(p.direct)
	if next.direct == nil {
		next.direct = make(map[uint64]directPeerFilters)
	}
	next.direct[luid] = directPeerFilters{peer: remote, index: index, rules: ids}
	next.canonicalize()
	return next, nil
}

// The raw probe socket remains active alongside the signed application grants.
// Install its two zero-port UDP exceptions only for online maintenance.
func (p FilterPlan) withConfirmedPeer(a candidateAssignment, candidate adapterIdentity, offline bool) (FilterPlan, error) {
	local, remote, err := a.addresses()
	if err != nil {
		return FilterPlan{}, err
	}
	probeLocal, probeRemote, err := a.controlAddresses()
	if err != nil || a.plan.LocalProbeIPv4 == "" || a.plan.PeerProbeIPv4 == "" {
		return FilterPlan{}, ErrNoAuthority
	}
	next, err := p.withDirectPeer(local, remote, candidate.LUID, candidate.Index)
	if err != nil {
		return FilterPlan{}, err
	}
	filters := next.direct[candidate.LUID]
	filters.peerID, filters.profileID = a.peer.ID, a.offer.Profile.ID
	filters.localEndpoint, filters.peerEndpoint = a.localEndpoint, a.peerEndpoint
	filters.adapter = candidate.GUID
	if !offline {
		for _, layer := range []wf.LayerID{wf.LayerALEAuthConnectV4, wf.LayerALEAuthRecvAcceptV4} {
			rule := &wf.Rule{Layer: layer, Action: wf.ActionPermit, Weight: 2, Conditions: []*wf.Match{
				{Field: wf.FieldIPLocalInterface, Op: wf.MatchTypeEqual, Value: candidate.LUID},
				{Field: wf.FieldIPLocalAddress, Op: wf.MatchTypeEqual, Value: probeLocal},
				{Field: wf.FieldIPRemoteAddress, Op: wf.MatchTypeEqual, Value: probeRemote},
				{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: wf.IPProto(17)},
				{Field: wf.FieldIPLocalPort, Op: wf.MatchTypeEqual, Value: uint16(0)},
				{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeEqual, Value: uint16(0)},
			}}
			next.addRule(rule)
			filters.rules[rule.ID] = true
		}
		next.canonicalize()
	}
	next.direct[candidate.LUID] = filters
	return next, nil
}

// A map refresh may already have removed the candidate's grants. Filter the
// currently applied plan so retiring one peer cannot resurrect old ACLs.
func (p FilterPlan) withoutDirectPeer(peer netip.Addr, luid uint64) (FilterPlan, error) {
	owner, ok := p.direct[luid]
	if !ok {
		return p, nil
	}
	if owner.peer != peer {
		return FilterPlan{}, ErrPolicyOwnership
	}
	next := p
	next.direct = maps.Clone(p.direct)
	delete(next.direct, luid)
	next.rules = make([]*wf.Rule, 0, len(p.rules))
	for _, rule := range p.rules {
		if !owner.rules[rule.ID] {
			next.rules = append(next.rules, rule)
		}
	}
	return next, nil
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
