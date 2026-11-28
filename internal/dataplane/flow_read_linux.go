package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os/exec"
	"strconv"
	"strings"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	wire "github.com/endless-net/coordinator/api/public/v1"
)

var errFlowSnapshot = errors.New("incomplete native flow counter snapshot")

var linuxFlowClasses = map[string]connectivity.FilterClass{
	"input_accept":  {Direction: wire.FlowDirection_FLOW_DIRECTION_INGRESS, Decision: "allow"},
	"input_drop":    {Direction: wire.FlowDirection_FLOW_DIRECTION_INGRESS, Decision: "deny"},
	"output_accept": {Direction: wire.FlowDirection_FLOW_DIRECTION_EGRESS, Decision: "allow"},
	"output_drop":   {Direction: wire.FlowDirection_FLOW_DIRECTION_EGRESS, Decision: "deny"},
}

// The kernel can advance counters during a dump. Only publish two identical
// observations from the same table and counter generation.
func readLinuxFlowSnapshot(ctx context.Context, owner state.LinuxNetworkReceipt, maxBytes int) (connectivity.FilterSnapshot, error) {
	return readLinuxCounters(ctx, owner, maxBytes, 0)
}

// An owning session serializes arm, seal, and removal of its observation
// rules. Removing the live gate stops increments without changing the ACL.
// If it already expired, the final sample cannot prove continuous coverage.
func sealLinuxFlowSnapshot(ctx context.Context, owner state.LinuxNetworkReceipt, before connectivity.FilterSnapshot, maxBytes int) (connectivity.FilterSnapshot, error) {
	var zero connectivity.FilterSnapshot
	if maxBytes < 1 || before.Generation == 0 || before.TableHandle == 0 {
		return zero, ErrConfiguration
	}
	live, err := dumpLinuxFlowCounters(ctx, owner, maxBytes, 0)
	if err != nil {
		return zero, err
	}
	if live.TableHandle != before.TableHandle || live.Generation != before.Generation {
		return zero, errFlowSnapshot
	}
	// The session creates the TUN and table with the same unique name.
	if err := linuxNFTApply(ctx, "delete element inet "+owner.Table+" flow_interfaces { "+strconv.Quote(owner.Table)+" }\n"); err != nil {
		return zero, err
	}
	return readLinuxCounters(ctx, owner, maxBytes, before.Generation)
}

func dumpLinuxFlowCounters(ctx context.Context, owner state.LinuxNetworkReceipt, maxBytes int, sealedGeneration uint64) (connectivity.FilterSnapshot, error) {
	var zero connectivity.FilterSnapshot
	if !linuxObjectName.MatchString(owner.Table) || owner.Owner == "" || maxBytes < 1 {
		return zero, ErrConfiguration
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(child, linuxNFT, "-a", "-j", "list", "table", "inet", owner.Table)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return zero, err
	}
	if err = cmd.Start(); err != nil {
		return zero, err
	}
	data, err := io.ReadAll(io.LimitReader(pipe, int64(maxBytes)))
	if err != nil || len(data) == maxBytes {
		cancel()
		_ = cmd.Wait()
		return zero, errFlowSnapshot
	}
	if err = cmd.Wait(); err != nil {
		return zero, err
	}
	return decodeLinuxFlowSnapshot(data, owner, sealedGeneration)
}

func readLinuxCounters(ctx context.Context, owner state.LinuxNetworkReceipt, maxBytes int, sealedGeneration uint64) (connectivity.FilterSnapshot, error) {
	var zero connectivity.FilterSnapshot
	a, err := dumpLinuxFlowCounters(ctx, owner, maxBytes, sealedGeneration)
	if err != nil {
		return zero, err
	}
	b, err := dumpLinuxFlowCounters(ctx, owner, maxBytes, sealedGeneration)
	if err != nil {
		return zero, err
	}
	if a.TableHandle != b.TableHandle || a.Generation != b.Generation || !maps.Equal(a.Totals, b.Totals) || !maps.Equal(a.Details, b.Details) {
		return zero, errFlowSnapshot
	}
	return b, nil
}

type linuxFlowElement struct {
	Val     struct{ Concat []json.RawMessage }
	Counter *connectivity.PacketCounts
}

func decodeLinuxFlowSnapshot(data []byte, owner state.LinuxNetworkReceipt, sealedGeneration uint64) (connectivity.FilterSnapshot, error) {
	var zero connectivity.FilterSnapshot
	var doc struct {
		Entries []struct {
			Table *linuxNFTTable
			Set   *struct {
				Name, Family, Table string
				Handle              uint64
				Elem                []json.RawMessage
			}
			Rule *struct {
				Family, Table, Chain string
				Expr                 []struct{ Counter *connectivity.PacketCounts }
			}
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return zero, err
	}
	out := connectivity.FilterSnapshot{Totals: map[connectivity.FilterClass]connectivity.PacketCounts{}, Details: map[connectivity.FilterTuple]connectivity.PacketCounts{}}
	sets := map[string]bool{}
	for _, entry := range doc.Entries {
		if t := entry.Table; t != nil {
			if t.Name != owner.Table || t.Family != "inet" || t.Comment != "endlessnet:"+owner.Owner || t.Handle == 0 || out.TableHandle != 0 {
				return zero, ErrPolicyOwnership
			}
			out.TableHandle = t.Handle
		}
		if s := entry.Set; s != nil && s.Name == "flow_interfaces" {
			var gate struct {
				Elem struct {
					Val     string
					Expires uint64
				}
			}
			if s.Family != "inet" || s.Table != owner.Table || s.Handle == 0 || out.Generation != 0 {
				return zero, errFlowSnapshot
			}
			if sealedGeneration != 0 {
				if s.Handle != sealedGeneration || len(s.Elem) != 0 {
					return zero, errFlowSnapshot
				}
			} else if len(s.Elem) != 1 || json.Unmarshal(s.Elem[0], &gate) != nil || gate.Elem.Val != owner.Table || gate.Elem.Expires == 0 {
				return zero, errFlowSnapshot
			}
			out.Generation = s.Handle
		}
		if s := entry.Set; s != nil && strings.HasPrefix(s.Name, "seen_") {
			class, ok := linuxFlowClasses[strings.TrimPrefix(s.Name, "seen_")]
			if !ok || s.Family != "inet" || s.Table != owner.Table || s.Handle == 0 || sets[s.Name] {
				return zero, errFlowSnapshot
			}
			sets[s.Name] = true
			for _, elem := range s.Elem {
				var wrapped struct{ Elem linuxFlowElement }
				if err := json.Unmarshal(elem, &wrapped); err != nil {
					return zero, err
				}
				row := wrapped.Elem
				key := connectivity.FilterTuple{FilterClass: class}
				parts := row.Val.Concat
				if len(parts) != 4 || row.Counter == nil || json.Unmarshal(parts[0], &key.Source) != nil || json.Unmarshal(parts[1], &key.Destination) != nil || json.Unmarshal(parts[3], &key.DestinationPort) != nil || !key.Source.Is4() || !key.Destination.Is4() {
					return zero, errFlowSnapshot
				}
				// nft can render inet_proto as its IANA number or service name.
				switch string(parts[2]) {
				case "6", `"tcp"`:
					key.Protocol = "tcp"
				case "17", `"udp"`:
					key.Protocol = "udp"
				default:
					return zero, errFlowSnapshot
				}
				if _, exists := out.Details[key]; exists {
					return zero, errFlowSnapshot
				}
				out.Details[key] = *row.Counter
			}
		}
		if r := entry.Rule; r != nil {
			class, ok := linuxFlowClasses[r.Chain]
			if !ok {
				continue
			}
			if r.Family != "inet" || r.Table != owner.Table {
				return zero, errFlowSnapshot
			}
			for _, expr := range r.Expr {
				if expr.Counter != nil {
					if _, exists := out.Totals[class]; exists {
						return zero, errFlowSnapshot
					}
					out.Totals[class] = *expr.Counter
				}
			}
		}
	}
	if out.TableHandle == 0 || out.Generation == 0 || len(sets) != 4 || len(out.Totals) != 4 {
		return zero, errFlowSnapshot
	}
	return out, nil
}
