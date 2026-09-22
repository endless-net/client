package client

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitLANBPFDescribeOwnershipUsesHeldObjectsAndRevokes(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
			p, _, k := newExitLANBPFPinFixture(t, order, mode)
			t.Cleanup(func() { _ = p.Close() })
			d, clock := exitLANBPFTestDeadline(t)
			if err := publishExitLANBPFTestDeadline(p, t.Context(), d, clock); err != nil {
				t.Fatal(err)
			}
			o, err := p.describeOwnership(t.Context(), exitLANBPFTestScope, exitLANOwnershipFixture(mode).BootID, 4, 5)
			if err != nil || validateExitLANOwnership(o) != nil {
				t.Fatal("failed held description", err)
			}
			if o.MapID != 200 || o.ProgramID != 100 || o.Family != mode || len(o.Links) != len(p.links) || k.link.k.objects[p.outer].slot != 0 || len(k.pins) != 0 {
				t.Fatal("description invented IDs, retained lease or created pins")
			}
			for i, link := range o.Links {
				if link.ID != p.links[i].identity.id {
					t.Fatal("incorrect held link")
				}
			}
		}
	}
}

func TestExitLANBPFDescribeOwnershipFailsWithoutReceipt(t *testing.T) {
	for _, scenario := range []string{"scope", "boot", "namespace", "pre_cancel", "late_cancel", "metadata", "map_changed", "revoke_failure"} {
		t.Run(scenario, func(t *testing.T) {
			p, _, k := newExitLANBPFPinFixture(t, binary.LittleEndian, api.ExitFamilyDualStack)
			t.Cleanup(func() { _ = p.Close() })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			scope, boot, device := exitLANBPFTestScope, exitLANOwnershipFixture(api.ExitFamilyDualStack).BootID, uint64(4)
			before := k.steps
			switch scenario {
			case "scope":
				scope = "bad"
			case "boot":
				boot = "bad"
			case "namespace":
				device = 0
			case "pre_cancel":
				cancel()
			case "late_cancel":
				k.after = func(int) { cancel() }
			case "metadata":
				id := k.link.links[p.links[0].fd]
				id.id++
				k.link.links[p.links[0].fd] = id
			case "map_changed":
				original := p.call
				reads := 0
				p.call = func(command int, attr []byte, buffers ...[]byte) (int, error) {
					fd, err := original(command, attr, buffers...)
					if command == exitLANBPFInfo && binary.LittleEndian.Uint32(attr) == uint32(p.outer) {
						reads++
						if reads == 2 {
							binary.LittleEndian.PutUint32(buffers[0][4:], 201)
						}
					}
					return fd, err
				}
			case "revoke_failure":
				k.fail = k.steps + 1
			}
			o, err := p.describeOwnership(ctx, scope, boot, device, 5)
			if err == nil || o != nil {
				t.Fatal("uncertain description returned receipt")
			}
			if (scenario == "scope" || scenario == "boot" || scenario == "namespace" || scenario == "pre_cancel") && k.steps != before {
				t.Fatal("invalid input reached kernel")
			}
			if (scenario == "pre_cancel" || scenario == "late_cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
		})
	}
}
