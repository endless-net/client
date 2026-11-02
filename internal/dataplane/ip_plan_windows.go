//go:build windows

package dataplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var ErrIP = errors.New("IP configuration failed")
var ErrIPConflict = errors.New("IP configuration conflict")
var ErrIPOwnership = errors.New("IP configuration ownership changed")
var ErrIPUncertain = errors.New("IP configuration outcome uncertain")
var ErrIPPermission = errors.New("IP configuration permission denied")
var ErrIPLimit = errors.New("IP configuration inventory limit exceeded")

// IPPlan describes only one address and exact peer /32 routes. No API here
// changes DNS, default routes, interface metrics, or unrelated table entries.
type IPPlan struct {
	adapter     windows.GUID
	luid        winipcfg.LUID
	address     winipcfg.MibUnicastIPAddressRow
	routes      []winipcfg.MibIPforwardRow2
	cleanupOnly bool
}

func (IPPlan) String() string   { return "[Windows IP plan]" }
func (IPPlan) GoString() string { return "[Windows IP plan]" }

// PrepareIPCleanup restores only object keys from the protected journal. It
// accepts no cached map/credential and cannot be used to create network effects.
func PrepareIPCleanup(adapterID string, luid uint64, localPrefix string) (IPPlan, error) {
	key, err := canonicalGUID(adapterID)
	prefix, parseErr := netip.ParsePrefix(localPrefix)
	if err != nil || luid == 0 || parseErr != nil || prefix.Bits() != 32 || !hostIPv4(prefix.Addr()) || prefix.String() != localPrefix {
		return IPPlan{}, ErrConfiguration
	}
	p := IPPlan{adapter: key, luid: winipcfg.LUID(luid), cleanupOnly: true}
	if p.address.Address.SetAddr(prefix.Addr()) != nil {
		return IPPlan{}, ErrConfiguration
	}
	return p, nil
}

func PrepareIPPlan(m connectivity.Map, adapterID string, luid uint64, metric uint32, maxPeers int) (IPPlan, error) {
	snapshot, err := m.Snapshot()
	if err != nil || m.Denied() {
		return IPPlan{}, ErrConfiguration
	}
	return ipPlan(snapshot, adapterID, luid, metric, maxPeers)
}

func ipPlan(m coordinator.NetworkMapSnapshot, adapterID string, luid uint64, metric uint32, maxPeers int) (IPPlan, error) {
	key, err := canonicalGUID(adapterID)
	if err != nil || luid == 0 || maxPeers <= 0 || len(m.Peers) > maxPeers {
		return IPPlan{}, ErrConfiguration
	}
	local, err := netip.ParseAddr(m.Node.AssignedIP)
	if err != nil || !hostIPv4(local) {
		return IPPlan{}, ErrConfiguration
	}
	p := IPPlan{adapter: key, luid: winipcfg.LUID(luid)}
	p.address.Init()
	p.address.InterfaceLUID = p.luid
	p.address.PrefixOrigin, p.address.SuffixOrigin = winipcfg.PrefixOriginManual, winipcfg.SuffixOriginManual
	p.address.OnLinkPrefixLength = 32
	p.address.SkipAsSource = false
	p.address.ValidLifetime, p.address.PreferredLifetime = ^uint32(0), ^uint32(0)
	if p.address.Address.SetAddr(local) != nil {
		return IPPlan{}, ErrConfiguration
	}
	addresses := map[netip.Addr]bool{local: true}
	endpoints := make([]netip.Addr, 0, len(m.Peers))
	for _, peer := range m.Peers {
		if len(peer.AllowedIPs) != 1 {
			return IPPlan{}, ErrConfiguration
		}
		prefix, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || !hostIPv4(prefix.Addr()) || prefix.Bits() != 32 || prefix.String() != peer.AllowedIPs[0] || addresses[prefix.Addr()] {
			return IPPlan{}, ErrConfiguration
		}
		endpoint, err := netip.ParseAddrPort(peer.Endpoint)
		if err != nil || !hostIPv4(endpoint.Addr()) || endpoint.Port() == 0 {
			return IPPlan{}, ErrConfiguration
		}
		endpoints = append(endpoints, endpoint.Addr())
		addresses[prefix.Addr()] = true
		var row winipcfg.MibIPforwardRow2
		row.Init()
		row.InterfaceLUID, row.Metric = p.luid, metric
		row.Protocol = winipcfg.RouteProtocolNetMgmt
		row.SitePrefixLength = 32
		row.Loopback, row.AutoconfigureAddress, row.Publish = false, false, false
		row.ValidLifetime, row.PreferredLifetime = ^uint32(0), ^uint32(0)
		if row.DestinationPrefix.SetPrefix(prefix) != nil || row.NextHop.SetAddr(netip.IPv4Unspecified()) != nil {
			return IPPlan{}, ErrConfiguration
		}
		p.routes = append(p.routes, row)
	}
	for _, endpoint := range endpoints {
		if addresses[endpoint] {
			return IPPlan{}, ErrIPConflict
		}
	}
	sort.Slice(p.routes, func(i, j int) bool {
		return p.routes[i].DestinationPrefix.Prefix().String() < p.routes[j].DestinationPrefix.Prefix().String()
	})
	return p, nil
}

// Prepared fingerprints stay empty until an acknowledged create/read receipt
// identifies the actual object. A prepared intent alone cannot authorize delete.
func (p IPPlan) Prepared() (state.AddressRecord, []state.RouteRecord) {
	a := state.AddressRecord{Prefix: netip.PrefixFrom(p.address.Address.Addr(), 32).String(), EffectStamp: state.EffectStamp{Phase: state.EffectPrepared}}
	routes := make([]state.RouteRecord, len(p.routes))
	for i, r := range p.routes {
		routes[i] = state.RouteRecord{Destination: r.DestinationPrefix.Prefix().String(), NextHop: r.NextHop.Addr().String(), Metric: r.Metric, EffectStamp: state.EffectStamp{Phase: state.EffectPrepared}}
	}
	return a, routes
}

func hostIPv4(a netip.Addr) bool {
	return a.Is4() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast()
}

func addressProof(adapter windows.GUID, r winipcfg.MibUnicastIPAddressRow) string {
	// CreationTimeStamp distinguishes deletion/recreation with the same key.
	// DAD and interface index are naturally variable and are checked separately.
	value := struct {
		Adapter          windows.GUID
		LUID             uint64
		Address          string
		Prefix           uint8
		PrefixOrigin     winipcfg.PrefixOrigin
		SuffixOrigin     winipcfg.SuffixOrigin
		SkipAsSource     bool
		ScopeID          uint32
		Creation         int64
		Valid, Preferred uint32
	}{adapter, uint64(r.InterfaceLUID), r.Address.Addr().String(), r.OnLinkPrefixLength, r.PrefixOrigin, r.SuffixOrigin, r.SkipAsSource, r.ScopeID, r.CreationTimeStamp, r.ValidLifetime, r.PreferredLifetime}
	return ipProof(value)
}

func routeProof(adapter windows.GUID, r winipcfg.MibIPforwardRow2) string {
	value := struct {
		Adapter                                    windows.GUID
		LUID                                       uint64
		Destination, NextHop                       string
		Metric                                     uint32
		SitePrefix                                 uint8
		Protocol                                   winipcfg.RouteProtocol
		Origin                                     winipcfg.RouteOrigin
		Loopback, Autoconfigure, Publish, Immortal bool
		Valid, Preferred                           uint32
	}{adapter, uint64(r.InterfaceLUID), r.DestinationPrefix.Prefix().String(), r.NextHop.Addr().String(), r.Metric, r.SitePrefixLength, r.Protocol, r.Origin, r.Loopback, r.AutoconfigureAddress, r.Publish, r.Immortal, r.ValidLifetime, r.PreferredLifetime}
	return ipProof(value)
}

func ipProof(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func addressMatches(p IPPlan, r winipcfg.MibUnicastIPAddressRow, record state.AddressRecord) bool {
	return record.Fingerprint != "" && record.Fingerprint == addressProof(p.adapter, r) && r.InterfaceLUID == p.luid && netip.PrefixFrom(r.Address.Addr(), int(r.OnLinkPrefixLength)).String() == record.Prefix
}

func routeMatches(p IPPlan, r winipcfg.MibIPforwardRow2, record state.RouteRecord) bool {
	return record.Fingerprint != "" && record.Fingerprint == routeProof(p.adapter, r) && r.InterfaceLUID == p.luid && r.DestinationPrefix.Prefix().String() == record.Destination && r.NextHop.Addr().String() == record.NextHop && r.Metric == record.Metric
}
