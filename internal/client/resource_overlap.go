package client

import (
	"context"
	"errors"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func resourceRulesOverlap(a, b resourceDenyRule) bool {
	return a.prefix.Overlaps(b.prefix) && (a.protocol == 0 || b.protocol == 0 || a.protocol == b.protocol) && (a.port == 0 || b.port == 0 || a.port == b.port)
}

// Called for the entire authenticated catalog before search and pagination,
// so a filtered-out row cannot hide a conflicting disclosed resource.
func projectResourceOverlaps(ctx context.Context, source *api.RegisterNodeResponse, items []*ipc.Resource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	footprints := make([][]resourceDenyRule, len(items))
	total := 0
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := resolveResourceInAuthenticatedMap(source, item.Id)
		if err != nil {
			return err
		}
		footprints[i], err = resourcePacketFootprint(source, identity)
		if err != nil {
			return err
		}
		total += len(footprints[i])
		if total > 8192 {
			return errors.New("resource footprint limit exceeded")
		}
	}
	comparisons, links := 0, 0
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			overlaps := false
			for _, a := range footprints[i] {
				for _, b := range footprints[j] {
					comparisons++
					if comparisons > 1000000 {
						return errors.New("resource overlap comparison limit exceeded")
					}
					if resourceRulesOverlap(a, b) {
						overlaps = true
						break
					}
				}
				if overlaps {
					break
				}
			}
			if !overlaps {
				continue
			}
			links += 2
			if links > 32768 {
				return errors.New("resource overlap result limit exceeded")
			}
			items[i].OverlappingResourceIds = append(items[i].OverlappingResourceIds, items[j].Id)
			items[j].OverlappingResourceIds = append(items[j].OverlappingResourceIds, items[i].Id)
			items[i].OverlapReasonKey = "resource_packet_scope_overlap"
			items[j].OverlapReasonKey = "resource_packet_scope_overlap"
		}
	}
	return nil
}
