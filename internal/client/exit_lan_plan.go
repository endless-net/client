package client

import (
	"net/netip"
	"slices"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Prepared destinations retain their original connected prefix and source
// addresses. They do not authorize native effects: packet-time binding, kernel
// expiry, physical-platform qualification and exit health are still required.
type exitLANBinding struct {
	linkIndex    int
	linkName     string
	connected    netip.Prefix
	sources      []netip.Addr
	destinations []netip.Prefix
}

type exitLANPlan struct {
	topology *exitLANSource
	bindings []exitLANBinding
	expires  time.Time
}

func compileExitLANPlan(cfg Config, source api.RegisterNodeResponse, selection *ClientExitSelection, topology *exitLANSource, retained []netip.Prefix, now time.Time) (*exitLANPlan, error) {
	if topology == nil || !exitLANInterfaceName(topology.OwnInterface) || topology.OwnInterface == "lo" || len(topology.Links) == 0 || len(topology.Links) > 128 || !topology.ValidUntil.IsZero() && !now.Before(topology.ValidUntil) {
		return nil, errExitLANPolicy
	}
	reservation, err := compileExitLANReservation(cfg, source, selection, retained, now)
	if err != nil {
		return nil, err
	}
	copy := *topology
	copy.Links = slices.Clone(topology.Links)
	plan := &exitLANPlan{topology: &copy, expires: reservation.expires}
	if !topology.ValidUntil.IsZero() && topology.ValidUntil.Before(plan.expires) {
		plan.expires = topology.ValidUntil
	}
	seen := map[int]bool{}
	ipv4, ipv6 := false, false
	totalFragments := 0
	work := 0
	for i, link := range topology.Links {
		if link.Index <= 0 || link.LinkIndex != link.Index || seen[link.Index] || !exitLANInterfaceName(link.Name) || link.Name == topology.OwnInterface || link.Name == "lo" || len(link.Addresses) > 128 || len(link.Routes) > 4096 {
			return nil, errExitLANPolicy
		}
		seen[link.Index] = true
		copy.Links[i].Addresses = slices.Clone(link.Addresses)
		copy.Links[i].Routes = slices.Clone(link.Routes)
		for _, route := range link.Routes {
			work++
			if work > exitLANWorkLimit {
				return nil, errExitLANPolicy
			}
			prefix := route.Prefix
			if !prefix.IsValid() || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
				return nil, errExitLANPolicy
			}
			if prefix.Addr().Is4() && selection.Family == api.ExitFamilyIPv6Only || prefix.Addr().Is6() && selection.Family == api.ExitFamilyIPv4Only {
				continue
			}
			for _, binding := range plan.bindings {
				work++
				if work > exitLANWorkLimit {
					return nil, errExitLANPolicy
				}
				if binding.connected.Overlaps(prefix) {
					return nil, errExitLANPolicy
				}
			}
			binding := exitLANBinding{linkIndex: link.Index, linkName: link.Name, connected: prefix}
			for _, address := range link.Addresses {
				work++
				if work > exitLANWorkLimit {
					return nil, errExitLANPolicy
				}
				if !address.IsValid() || address.Addr().Is4In6() {
					return nil, errExitLANPolicy
				}
				if address.Masked() == prefix {
					binding.sources = append(binding.sources, address.Addr())
				}
			}
			if len(binding.sources) == 0 {
				return nil, errExitLANPolicy
			}
			binding.destinations, err = reservation.destinationsWithinBudget(prefix, now, &work)
			if err != nil {
				return nil, err
			}
			if len(binding.destinations) == 0 {
				continue
			}
			totalFragments += len(binding.destinations)
			if totalFragments > exitLANPrefixLimit {
				return nil, errExitLANPolicy
			}
			ipv4 = ipv4 || prefix.Addr().Is4()
			ipv6 = ipv6 || prefix.Addr().Is6()
			plan.bindings = append(plan.bindings, binding)
		}
	}
	if selection.Family != api.ExitFamilyIPv6Only && !ipv4 || selection.Family != api.ExitFamilyIPv4Only && !ipv6 {
		return nil, errExitLANPolicy
	}
	return plan, nil
}
