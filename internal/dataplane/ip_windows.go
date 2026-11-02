//go:build windows

package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"sync"

	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type ipAPI interface {
	guid(winipcfg.LUID) (*windows.GUID, error)
	addresses() ([]winipcfg.MibUnicastIPAddressRow, error)
	routes() ([]winipcfg.MibIPforwardRow2, error)
	address(winipcfg.LUID, netip.Addr) (*winipcfg.MibUnicastIPAddressRow, error)
	route(winipcfg.LUID, netip.Prefix, netip.Addr) (*winipcfg.MibIPforwardRow2, error)
	createAddress(*winipcfg.MibUnicastIPAddressRow) error
	createRoute(*winipcfg.MibIPforwardRow2) error
	deleteAddress(*winipcfg.MibUnicastIPAddressRow) error
	deleteRoute(*winipcfg.MibIPforwardRow2) error
}

type nativeIPAPI struct{}

func (nativeIPAPI) guid(l winipcfg.LUID) (*windows.GUID, error) { return l.GUID() }
func (nativeIPAPI) addresses() ([]winipcfg.MibUnicastIPAddressRow, error) {
	return winipcfg.GetUnicastIPAddressTable(windows.AF_INET)
}
func (nativeIPAPI) routes() ([]winipcfg.MibIPforwardRow2, error) {
	return winipcfg.GetIPForwardTable2(windows.AF_INET)
}
func (nativeIPAPI) address(l winipcfg.LUID, a netip.Addr) (*winipcfg.MibUnicastIPAddressRow, error) {
	return l.IPAddress(a)
}
func (nativeIPAPI) route(l winipcfg.LUID, p netip.Prefix, n netip.Addr) (*winipcfg.MibIPforwardRow2, error) {
	return l.Route(p, n)
}
func (nativeIPAPI) createAddress(r *winipcfg.MibUnicastIPAddressRow) error { return r.Create() }
func (nativeIPAPI) createRoute(r *winipcfg.MibIPforwardRow2) error         { return r.Create() }
func (nativeIPAPI) deleteAddress(r *winipcfg.MibUnicastIPAddressRow) error { return r.Delete() }
func (nativeIPAPI) deleteRoute(r *winipcfg.MibIPforwardRow2) error         { return r.Delete() }

// WindowsIP edits individual rows only. Its caller persists prepared/removing
// journal entries before each mutation, commits returned receipts before native
// activation, and keeps the tunnel denied on any error or uncertain outcome.
type WindowsIP struct {
	mu      sync.Mutex
	api     ipAPI
	maxRows int
}

func (*WindowsIP) String() string   { return "[Windows IP adapter]" }
func (*WindowsIP) GoString() string { return "[Windows IP adapter]" }
func NewWindowsIP(maxInventoryRows int) (*WindowsIP, error) {
	if maxInventoryRows <= 0 {
		return nil, ErrConfiguration
	}
	return &WindowsIP{api: nativeIPAPI{}, maxRows: maxInventoryRows}, nil
}

type AddressEffect struct {
	Record state.AddressRecord
	Ready  bool
}

func ipError(err error) error {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return ErrIPPermission
	}
	if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return ErrIPConflict
	}
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return ErrConfiguration
	}
	return ErrIP
}
func ipMissing(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_FOUND) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND)
}

func (w *WindowsIP) guard(ctx context.Context, p IPPlan) error {
	if ctx == nil || w.api == nil || w.maxRows <= 0 || p.luid == 0 || p.adapter == (windows.GUID{}) {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	id, err := w.api.guid(p.luid)
	if err != nil {
		return ipError(err)
	}
	if id == nil || *id != p.adapter {
		return ErrIPOwnership
	}
	return ctx.Err()
}

func (w *WindowsIP) inventory(ctx context.Context) ([]winipcfg.MibUnicastIPAddressRow, []winipcfg.MibIPforwardRow2, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	addresses, err := w.api.addresses()
	if err != nil {
		return nil, nil, ipError(err)
	}
	if len(addresses) > w.maxRows {
		return nil, nil, ErrIPLimit
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	routes, err := w.api.routes()
	if err != nil {
		return nil, nil, ipError(err)
	}
	if len(routes) > w.maxRows-len(addresses) {
		return nil, nil, ErrIPLimit
	}
	return addresses, routes, ctx.Err()
}

// Preflight rejects any non-default route that would be overridden for a device
// address, and any conflicting local assignment. Only exact journal proofs may
// exempt an existing own row. The check is repeated before each individual add;
// it is not a reservation against later administrative changes.
func (w *WindowsIP) Preflight(ctx context.Context, p IPPlan, j state.ResourceJournal) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.preflight(ctx, p, j)
}
func (w *WindowsIP) preflight(ctx context.Context, p IPPlan, j state.ResourceJournal) error {
	if err := w.guard(ctx, p); err != nil {
		return err
	}
	if err := journalBinding(p, j); err != nil {
		return err
	}
	addresses, routes, err := w.inventory(ctx)
	if err != nil {
		return err
	}
	targets := map[netip.Addr]bool{p.address.Address.Addr(): true}
	for _, r := range p.routes {
		targets[r.DestinationPrefix.Prefix().Addr()] = true
	}
	for _, r := range addresses {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !targets[r.Address.Addr()] {
			continue
		}
		owned := false
		for _, record := range j.Addresses {
			if addressMatches(p, r, record) {
				owned = true
				break
			}
		}
		if !owned {
			return ErrIPConflict
		}
	}
	for _, r := range routes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prefix := r.DestinationPrefix.Prefix()
		if !prefix.IsValid() || !prefix.Addr().Is4() {
			return ErrIP
		}
		if prefix.Bits() == 0 {
			continue
		} // preserve the existing internet route
		conflict := false
		for target := range targets {
			if prefix.Contains(target) {
				conflict = true
				break
			}
		}
		if !conflict {
			continue
		}
		owned := false
		for _, record := range j.Routes {
			if routeMatches(p, r, record) {
				owned = true
				break
			}
		}
		if !owned {
			for _, address := range j.Addresses {
				if address.AutomaticRoute != nil && routeMatches(p, r, *address.AutomaticRoute) {
					owned = true
					break
				}
			}
		}
		if !owned {
			return ErrIPConflict
		}
	}
	return nil
}
func journalBinding(p IPPlan, j state.ResourceJournal) error {
	id, err := canonicalGUID(j.ID)
	if err != nil || id != p.adapter || j.Adapter == nil || j.Adapter.ID != j.ID || j.Adapter.LUID != uint64(p.luid) || j.Adapter.Phase != state.EffectApplied || j.Adapter.Fingerprint == "" {
		return ErrIPOwnership
	}
	return nil
}

func (w *WindowsIP) CreateAddress(ctx context.Context, p IPPlan, j state.ResourceJournal) (AddressEffect, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p.cleanupOnly {
		return AddressEffect{}, ErrConfiguration
	}
	expected, _ := p.Prepared()
	found := false
	for _, r := range j.Addresses {
		if r == expected {
			found = true
		}
	}
	if !found {
		return AddressEffect{}, ErrConfiguration
	}
	if err := w.preflight(ctx, p, j); err != nil {
		return AddressEffect{}, err
	}
	if _, err := w.api.address(p.luid, p.address.Address.Addr()); err == nil {
		return AddressEffect{}, ErrIPOwnership
	} else if !ipMissing(err) {
		return AddressEffect{}, ipError(err)
	}
	if err := w.guard(ctx, p); err != nil {
		return AddressEffect{}, err
	}
	row := p.address
	if err := w.api.createAddress(&row); err != nil {
		if reason := ipError(err); reason != ErrIP {
			return AddressEffect{}, reason
		}
		return AddressEffect{}, ErrIPUncertain
	}
	if ctx.Err() != nil {
		return AddressEffect{}, ErrIPUncertain
	}
	actual, err := w.api.address(p.luid, p.address.Address.Addr())
	if err != nil || actual == nil {
		return AddressEffect{}, ErrIPUncertain
	}
	if !addressShape(p, *actual) {
		return AddressEffect{}, ErrIPOwnership
	}
	record := state.AddressRecord{Prefix: expected.Prefix, EffectStamp: state.EffectStamp{Phase: state.EffectApplied, Fingerprint: addressProof(p.adapter, *actual)}}
	return w.addressEffect(ctx, p, *actual, record)
}

func addressShape(p IPPlan, r winipcfg.MibUnicastIPAddressRow) bool {
	return r.InterfaceLUID == p.luid && r.Address.Addr() == p.address.Address.Addr() && r.OnLinkPrefixLength == 32 && r.PrefixOrigin == winipcfg.PrefixOriginManual && r.SuffixOrigin == winipcfg.SuffixOriginManual && !r.SkipAsSource && r.ValidLifetime == ^uint32(0) && r.PreferredLifetime == ^uint32(0) && r.CreationTimeStamp > 0
}

func (w *WindowsIP) InspectAddress(ctx context.Context, p IPPlan, record state.AddressRecord) (AddressEffect, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.guard(ctx, p); err != nil {
		return AddressEffect{}, err
	}
	r, err := w.api.address(p.luid, p.address.Address.Addr())
	if err != nil {
		return AddressEffect{}, ipError(err)
	}
	if r == nil || !addressMatches(p, *r, record) {
		return AddressEffect{}, ErrIPOwnership
	}
	return w.addressEffect(ctx, p, *r, record)
}

func (w *WindowsIP) addressEffect(ctx context.Context, p IPPlan, r winipcfg.MibUnicastIPAddressRow, record state.AddressRecord) (AddressEffect, error) {
	effect := AddressEffect{Record: record}
	if ctx.Err() != nil {
		return effect, ErrIPUncertain
	}
	routes, err := w.api.routes()
	if err != nil || len(routes) > w.maxRows {
		return effect, ErrIPUncertain
	}
	found := false
	for _, route := range routes {
		if route.InterfaceLUID != p.luid || route.DestinationPrefix.Prefix() != netip.PrefixFrom(p.address.Address.Addr(), 32) {
			continue
		}
		if route.Protocol != winipcfg.RouteProtocolLocal || route.NextHop.Addr() != netip.IPv4Unspecified() {
			return effect, ErrIPOwnership
		}
		if found {
			return effect, ErrIPOwnership
		}
		found = true
		if previous := record.AutomaticRoute; previous != nil && !routeMatches(p, route, *previous) {
			return effect, ErrIPOwnership
		}
		effect.Record.AutomaticRoute = &state.RouteRecord{Destination: route.DestinationPrefix.Prefix().String(), NextHop: route.NextHop.Addr().String(), Metric: route.Metric, EffectStamp: state.EffectStamp{Phase: record.Phase, Fingerprint: routeProof(p.adapter, route)}}
		effect.Ready = r.DadState == winipcfg.DadStatePreferred
	}
	if record.AutomaticRoute != nil && !found {
		return effect, ErrIPOwnership
	}
	if r.DadState != winipcfg.DadStatePreferred && r.DadState != winipcfg.DadStateTentative {
		return effect, ErrIPConflict
	}
	if ctx.Err() != nil {
		return effect, ErrIPUncertain
	}
	return effect, nil
}

func (w *WindowsIP) CreateRoute(ctx context.Context, p IPPlan, prepared state.RouteRecord, j state.ResourceJournal) (state.RouteRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p.cleanupOnly {
		return state.RouteRecord{}, ErrConfiguration
	}
	var desired *winipcfg.MibIPforwardRow2
	for i := range p.routes {
		r := &p.routes[i]
		if r.DestinationPrefix.Prefix().String() == prepared.Destination && r.NextHop.Addr().String() == prepared.NextHop && r.Metric == prepared.Metric {
			desired = r
			break
		}
	}
	if desired == nil || prepared.Phase != state.EffectPrepared || prepared.Fingerprint != "" {
		return state.RouteRecord{}, ErrConfiguration
	}
	found := false
	for _, r := range j.Routes {
		if r == prepared {
			found = true
			break
		}
	}
	if !found {
		return state.RouteRecord{}, ErrConfiguration
	}
	if err := w.preflight(ctx, p, j); err != nil {
		return state.RouteRecord{}, err
	}
	if _, err := w.api.route(p.luid, desired.DestinationPrefix.Prefix(), desired.NextHop.Addr()); err == nil {
		return state.RouteRecord{}, ErrIPOwnership
	} else if !ipMissing(err) {
		return state.RouteRecord{}, ipError(err)
	}
	if err := w.guard(ctx, p); err != nil {
		return state.RouteRecord{}, err
	}
	row := *desired
	if err := w.api.createRoute(&row); err != nil {
		if reason := ipError(err); reason != ErrIP {
			return state.RouteRecord{}, reason
		}
		return state.RouteRecord{}, ErrIPUncertain
	}
	if ctx.Err() != nil {
		return state.RouteRecord{}, ErrIPUncertain
	}
	actual, err := w.api.route(p.luid, row.DestinationPrefix.Prefix(), row.NextHop.Addr())
	if err != nil || actual == nil {
		return state.RouteRecord{}, ErrIPUncertain
	}
	if !routeShape(*desired, *actual) {
		return state.RouteRecord{}, ErrIPOwnership
	}
	prepared.Phase, prepared.Fingerprint = state.EffectApplied, routeProof(p.adapter, *actual)
	if ctx.Err() != nil {
		return prepared, ErrIPUncertain
	}
	return prepared, nil
}

func routeShape(expected, actual winipcfg.MibIPforwardRow2) bool {
	return actual.InterfaceLUID == expected.InterfaceLUID && actual.DestinationPrefix.Prefix() == expected.DestinationPrefix.Prefix() && actual.NextHop.Addr() == expected.NextHop.Addr() && actual.Metric == expected.Metric && actual.Protocol == winipcfg.RouteProtocolNetMgmt && actual.Origin == winipcfg.RouteOriginManual && actual.SitePrefixLength == expected.SitePrefixLength && !actual.Loopback && !actual.AutoconfigureAddress && !actual.Publish && actual.ValidLifetime == ^uint32(0) && actual.PreferredLifetime == ^uint32(0)
}

func (w *WindowsIP) inspectRoute(ctx context.Context, p IPPlan, record state.RouteRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx == nil || w.api == nil || record.Phase != state.EffectApplied {
		return ErrConfiguration
	}
	if err := w.guard(ctx, p); err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(record.Destination)
	if err != nil {
		return ErrConfiguration
	}
	row, err := w.api.route(p.luid, prefix, netip.IPv4Unspecified())
	if err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if row == nil || !routeMatches(p, *row, record) {
		return ErrIPOwnership
	}
	return ctx.Err()
}

func (w *WindowsIP) RemoveRoute(ctx context.Context, p IPPlan, record state.RouteRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	prefix, err := netip.ParsePrefix(record.Destination)
	if err != nil || prefix.Bits() != 32 || !hostIPv4(prefix.Addr()) || record.NextHop != "0.0.0.0" || record.Phase != state.EffectRemoving || ctx == nil || w.api == nil {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r, err := w.api.route(p.luid, prefix, netip.IPv4Unspecified())
	if ipMissing(err) {
		return nil
	}
	if err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if r == nil || !routeMatches(p, *r, record) || r.Protocol != winipcfg.RouteProtocolNetMgmt {
		return ErrIPOwnership
	}
	if err := w.guard(ctx, p); err != nil {
		return err
	}
	if err := w.api.deleteRoute(r); err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if ctx.Err() != nil {
		return ErrIPUncertain
	}
	if _, err = w.api.route(p.luid, prefix, netip.IPv4Unspecified()); ipMissing(err) {
		return nil
	}
	return ErrIPUncertain
}

func (w *WindowsIP) RemoveAddress(ctx context.Context, p IPPlan, record state.AddressRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	prefix, err := netip.ParsePrefix(record.Prefix)
	if err != nil || prefix.Bits() != 32 || prefix.Addr() != p.address.Address.Addr() || record.Phase != state.EffectRemoving || ctx == nil || w.api == nil {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r, err := w.api.address(p.luid, prefix.Addr())
	if ipMissing(err) {
		return w.automaticAbsent(ctx, p)
	}
	if err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if r == nil || !addressMatches(p, *r, record) {
		return ErrIPOwnership
	}
	if err := w.guard(ctx, p); err != nil {
		return err
	}
	actual, err := w.api.route(p.luid, prefix, netip.IPv4Unspecified())
	if err == nil {
		a := record.AutomaticRoute
		if actual == nil || a == nil || a.Phase != state.EffectRemoving || !routeMatches(p, *actual, *a) {
			return ErrIPOwnership
		}
	} else if !ipMissing(err) {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := w.api.deleteAddress(r); err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	if ctx.Err() != nil {
		return ErrIPUncertain
	}
	if _, err := w.api.address(p.luid, prefix.Addr()); !ipMissing(err) {
		return ErrIPUncertain
	}
	return w.automaticAbsent(ctx, p)
}

func (w *WindowsIP) automaticAbsent(ctx context.Context, p IPPlan) error {
	if ctx.Err() != nil {
		return ErrIPUncertain
	}
	if _, err := w.api.route(p.luid, netip.PrefixFrom(p.address.Address.Addr(), 32), netip.IPv4Unspecified()); !ipMissing(err) {
		return ErrIPUncertain
	}
	return nil
}

func (w *WindowsIP) confirmFilterCleanup(ctx context.Context, p FilterPlan) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx == nil || w.api == nil || w.maxRows <= 0 || p.luid == 0 {
		return ErrConfiguration
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	id, err := w.api.guid(winipcfg.LUID(p.luid))
	if err == nil {
		if id == nil || *id != p.session {
			return ErrPolicyOwnership
		}
		return ErrPolicyCleanupRequired // remove the owned adapter before guards
	}
	if !ipMissing(err) {
		return errors.Join(ErrPolicyUncertain, ipError(err))
	}
	addresses, routes, err := w.inventory(ctx)
	if err != nil {
		return errors.Join(ErrPolicyUncertain, err)
	}
	for _, r := range addresses {
		if uint64(r.InterfaceLUID) == p.luid {
			return ErrPolicyCleanupRequired
		}
	}
	for _, r := range routes {
		if uint64(r.InterfaceLUID) == p.luid {
			return ErrPolicyCleanupRequired
		}
	}
	if ctx.Err() != nil {
		return ErrPolicyUncertain
	}
	return nil
}
