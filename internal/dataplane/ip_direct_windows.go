package dataplane

import (
	"context"
	"errors"
	"net/netip"

	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// The candidate adapter is owned by a live WireGuardNT handle. This receipt
// is scoped to that handle's route and the primary session whose route it shadows.
type windowsDirectRoute struct {
	primaryGUID windows.GUID
	primaryLUID winipcfg.LUID
	generation  uint64
	candidate   IPPlan
	record      state.RouteRecord
}

// The caller holds the candidate adapter open and installs the pair's filters
// before adding the preferred route. On an uncertain create, retire the adapter.
func (w *WindowsIP) installDirectRoute(ctx context.Context, primary IPPlan, j state.ResourceJournal, candidateGUID windows.GUID, candidateLUID winipcfg.LUID, peer netip.Addr) (*windowsDirectRoute, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !hostIPv4(peer) || primary.adapter == candidateGUID || primary.luid == candidateLUID || w.direct[peer] != nil {
		return nil, ErrIPConflict
	}
	if err := w.preflight(ctx, primary, j); err != nil {
		return nil, err
	}
	candidate := IPPlan{adapter: candidateGUID, luid: candidateLUID}
	if err := w.guard(ctx, candidate); err != nil {
		return nil, err
	}
	prefix := netip.PrefixFrom(peer, 32)
	row, err := w.api.route(primary.luid, prefix, netip.IPv4Unspecified())
	if err != nil || row == nil {
		return nil, ErrIPOwnership
	}
	// A retained journal entry alone cannot authorize a removed peer.
	var direct winipcfg.MibIPforwardRow2
	for _, desired := range primary.routes {
		if desired.DestinationPrefix.Prefix() == prefix && routeShape(desired, *row) {
			direct = desired
			break
		}
	}
	if direct.InterfaceLUID != primary.luid {
		return nil, ErrIPOwnership
	}
	direct.InterfaceLUID, direct.Metric = candidateLUID, 0
	for _, owned := range j.Routes {
		if owned.Phase != state.EffectApplied || !routeMatches(primary, *row, owned) {
			continue
		}
		receipt, err := w.createRouteReceipt(ctx, candidate, direct, state.RouteRecord{Destination: prefix.String(), NextHop: "0.0.0.0"})
		if err != nil {
			return nil, err
		}
		entry := &windowsDirectRoute{primaryGUID: primary.adapter, primaryLUID: primary.luid, generation: j.Generation, candidate: candidate, record: receipt}
		if w.direct == nil {
			w.direct = make(map[netip.Addr]*windowsDirectRoute)
		}
		w.direct[peer] = entry
		// A lower route metric alone does not guarantee selection: Windows
		// also accounts for the interface metric and competing routes.
		if err := w.preflight(ctx, primary, j); err != nil {
			if cleanupErr := w.retireDirectRouteLocked(context.WithoutCancel(ctx), peer, entry); cleanupErr != nil {
				return entry, errors.Join(err, cleanupErr)
			}
			return nil, err
		}
		return entry, nil
	}
	return nil, ErrIPOwnership
}

// An old close cannot remove a replacement candidate's identical /32.
func (w *WindowsIP) retireDirectRoute(ctx context.Context, peer netip.Addr, entry *windowsDirectRoute) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.retireDirectRouteLocked(ctx, peer, entry)
}

// Refresh calls this after replacing WFP grants and before removing primary
// routes. Keep the fallback row in place until its WireGuardNT shadow is gone.
func (w *WindowsIP) retireUnconfirmedDirectRoutes(ctx context.Context, plan FilterPlan, j state.ResourceJournal) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.direct) == 0 {
		return nil
	}
	primary := IPPlan{adapter: plan.session, luid: winipcfg.LUID(plan.luid)}
	if err := journalBinding(primary, j); err != nil {
		return err
	}
	for peer, entry := range w.direct {
		if entry.primaryGUID != primary.adapter || entry.primaryLUID != primary.luid || entry.generation != j.Generation {
			return ErrIPOwnership
		}
		owner, ok := plan.direct[uint64(entry.candidate.luid)]
		if ok && (owner.peer != peer || owner.adapter != entry.candidate.adapter) {
			return ErrIPOwnership
		}
		if !ok {
			if err := w.retireDirectRouteLocked(ctx, peer, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *WindowsIP) retireDirectRouteLocked(ctx context.Context, peer netip.Addr, entry *windowsDirectRoute) error {
	if entry == nil || w.direct[peer] != entry {
		return ErrIPOwnership
	}
	record := entry.record
	record.Phase = state.EffectRemoving
	if err := w.removeRoute(ctx, entry.candidate, record); err != nil {
		return err
	}
	delete(w.direct, peer)
	return nil
}
