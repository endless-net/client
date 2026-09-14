package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Confirmation covers the committed TUN denials only. A partial overlap is
// reported as a restriction; absence of a denial never becomes AVAILABLE.
func projectAppliedResourceDenials(ctx context.Context, cfg Config, items []*ipc.Resource, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rules, err := compileResourceDenials(cfg, now)
	if err != nil {
		return err
	}
	comparisons := 0
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
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
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, b := range rules {
				comparisons++
				if comparisons > 1000000 {
					return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
				}
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
