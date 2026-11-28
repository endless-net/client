package connectivity

import (
	"math"
	"net/netip"

	wire "github.com/endless-net/coordinator/api/public/v1"
)

// FilterSnapshot covers one generation of native observation rules. Totals
// count every packet before the bounded per-tuple counters are updated.
type FilterSnapshot struct {
	TableHandle uint64
	Generation  uint64
	Totals      map[FilterClass]PacketCounts
	Details     map[FilterTuple]PacketCounts
}

type FilterClass struct {
	Direction wire.FlowDirection
	Decision  string
}

type PacketCounts struct{ Packets, Bytes uint64 }

type FilterTuple struct {
	FilterClass
	Source, Destination netip.Addr
	Protocol            string
	DestinationPort     uint16
}

func (c FilterClass) valid() bool {
	return (c.Direction == wire.FlowDirection_FLOW_DIRECTION_INGRESS || c.Direction == wire.FlowDirection_FLOW_DIRECTION_EGRESS) && (c.Decision == "allow" || c.Decision == "deny")
}

func (n PacketCounts) delta(old PacketCounts) (PacketCounts, bool) {
	if n.Packets < old.Packets || n.Bytes < old.Bytes {
		return PacketCounts{}, false
	}
	d := PacketCounts{n.Packets - old.Packets, n.Bytes - old.Bytes}
	return d, (d.Packets == 0) == (d.Bytes == 0)
}

// Deltas are meaningful only within the same ruleset generation. Disappearing
// keys, decreasing counters and inconsistent totals require a fresh baseline.
func filterDeltas(before, after FilterSnapshot) ([]FilterCounter, []FilterClass, bool) {
	if before.Generation == 0 || before.Generation != after.Generation || before.TableHandle != after.TableHandle || len(before.Totals) != 4 || len(after.Totals) != 4 {
		return nil, nil, false
	}
	for k := range before.Details {
		if _, ok := after.Details[k]; !ok {
			return nil, nil, false
		}
	}
	details := make([]FilterCounter, 0, len(after.Details))
	accounted := make(map[FilterClass]PacketCounts, 4)
	for k, n := range after.Details {
		if !k.FilterClass.valid() || !k.Source.Is4() || !k.Destination.Is4() || (k.Protocol != "tcp" && k.Protocol != "udp") {
			return nil, nil, false
		}
		d, ok := n.delta(before.Details[k])
		if !ok {
			return nil, nil, false
		}
		if d.Packets == 0 {
			continue
		}
		sum := accounted[k.FilterClass]
		if sum.Packets > math.MaxUint64-d.Packets || sum.Bytes > math.MaxUint64-d.Bytes {
			return nil, nil, false
		}
		accounted[k.FilterClass] = PacketCounts{sum.Packets + d.Packets, sum.Bytes + d.Bytes}
		details = append(details, FilterCounter{k.Source, k.Destination, k.Protocol, k.DestinationPort, k.Direction, k.Decision, d.Packets, d.Bytes})
	}
	var partial []FilterClass
	for class, total := range after.Totals {
		old, ok := before.Totals[class]
		if !ok || !class.valid() {
			return nil, nil, false
		}
		d, ok := total.delta(old)
		if !ok {
			return nil, nil, false
		}
		untracked, ok := d.delta(accounted[class])
		if !ok {
			return nil, nil, false
		}
		if untracked.Packets > 0 {
			partial = append(partial, class)
		}
	}
	return details, partial, true
}
