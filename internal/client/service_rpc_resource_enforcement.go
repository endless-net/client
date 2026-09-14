package client

import (
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Confirmation covers the committed TUN denials only. A partial overlap is
// reported as a restriction; absence of a denial never becomes AVAILABLE.
func projectAppliedResourceDenials(cfg Config, items []*ipc.Resource, now time.Time) error {
	rules, err := compileResourceDenials(cfg, now)
	if err != nil {
		return err
	}
	for _, item := range items {
		identity, err := resolveResourceInAuthenticatedMap(cfg.CachedMap, item.Id)
		if err != nil {
			return err
		}
		footprint, err := resourcePacketFootprint(cfg.CachedMap, identity)
		if err != nil {
			return err
		}
		restricted := false
		for _, a := range footprint {
			for _, b := range rules {
				if resourceRulesOverlap(a, b) {
					restricted = true
					break
				}
			}
			if restricted {
				break
			}
		}
		if restricted {
			item.Availability = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "resource_packet_restriction_applied"}
		}
	}
	return nil
}
