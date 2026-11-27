package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/driver"
)

func blockWindowsDriverSocket(ctx context.Context, listener uint16, broker netip.AddrPort) (*nativeFilterAPI, error) {
	if listener == 0 || listener == broker.Port() || broker.Port() == 0 || broker.Addr() != netip.MustParseAddr("127.0.0.1") {
		return nil, ErrConfiguration
	}
	var rules []*wf.Rule
	add := func(layer wf.LayerID, extra ...*wf.Match) {
		conditions := []*wf.Match{{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: wf.IPProto(windows.IPPROTO_UDP)}, {Field: wf.FieldIPLocalPort, Op: wf.MatchTypeEqual, Value: listener}}
		rules = append(rules, &wf.Rule{Name: "EndlessNet private driver UDP", Layer: layer, Action: wf.ActionBlock, HardAction: true, Weight: 1, Conditions: append(conditions, extra...)})
	}
	add(wf.LayerDatagramDataV6)
	add(wf.LayerDatagramDataV4, &wf.Match{Field: wf.FieldIPLocalAddress, Op: wf.MatchTypeNotEqual, Value: broker.Addr()})
	add(wf.LayerDatagramDataV4, &wf.Match{Field: wf.FieldIPRemoteAddress, Op: wf.MatchTypeNotEqual, Value: broker.Addr()})
	add(wf.LayerDatagramDataV4, &wf.Match{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeNotEqual, Value: broker.Port()})
	return installCandidateBlocks(ctx, rules)
}

func supportedDriverSocketBlock(r *wf.Rule) bool {
	if r.Action != wf.ActionBlock || !r.HardAction || r.Persistent || r.Weight != 1 || len(r.Conditions) < 2 {
		return false
	}
	proto, port := r.Conditions[0], r.Conditions[1]
	if proto == nil || proto.Field != wf.FieldIPProtocol || proto.Op != wf.MatchTypeEqual || proto.Value != wf.IPProto(windows.IPPROTO_UDP) || port == nil || port.Field != wf.FieldIPLocalPort || port.Op != wf.MatchTypeEqual {
		return false
	}
	p, ok := port.Value.(uint16)
	if !ok || p == 0 {
		return false
	}
	if r.Layer == wf.LayerDatagramDataV6 {
		return len(r.Conditions) == 2
	}
	if len(r.Conditions) != 3 {
		return false
	}
	match := r.Conditions[2]
	if match == nil || match.Op != wf.MatchTypeNotEqual {
		return false
	}
	switch match.Field {
	case wf.FieldIPRemoteAddress, wf.FieldIPLocalAddress:
		return match.Value == netip.MustParseAddr("127.0.0.1")
	case wf.FieldIPRemotePort:
		remote, ok := match.Value.(uint16)
		return ok && remote != 0 && remote != p
	}
	return false
}

// The prepared adapter cannot carry application traffic while the supervisor
// is still deciding the pair's route. Both rules are installed in one dynamic
// session before activation or any address/route changes.
// Keep application addresses blocked on the candidate, including when an
// application binds an interface explicitly. At transport layers exclude all
// protocols and ports except the control codec's UDP 0 -> 0.
func windowsProbeRules(index uint32, local, remote netip.Addr) []*wf.Rule {
	var rules []*wf.Rule
	add := func(layer wf.LayerID, field wf.FieldID, value any) {
		rules = append(rules, &wf.Rule{
			Name: "EndlessNet Direct probe", Layer: layer, Action: wf.ActionBlock, HardAction: true, Weight: 1,
			Conditions: []*wf.Match{
				{Field: wf.FieldInterfaceIndex, Op: wf.MatchTypeEqual, Value: index},
				{Field: field, Op: wf.MatchTypeNotEqual, Value: value},
			},
		})
	}
	for _, layer := range []wf.LayerID{wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		add(layer, wf.FieldIPLocalAddress, local)
		add(layer, wf.FieldIPRemoteAddress, remote)
	}
	for _, layer := range []wf.LayerID{wf.LayerInboundTransportV4, wf.LayerOutboundTransportV4} {
		add(layer, wf.FieldIPProtocol, wf.IPProto(windows.IPPROTO_UDP))
		add(layer, wf.FieldIPLocalPort, uint16(0))
		add(layer, wf.FieldIPRemotePort, uint16(0))
	}
	return rules
}

func supportedProbeBlock(r *wf.Rule) bool {
	if r.Action != wf.ActionBlock || !r.HardAction || r.Persistent || r.Weight != 1 || len(r.Conditions) != 2 {
		return false
	}
	scope, condition := r.Conditions[0], r.Conditions[1]
	if scope == nil || scope.Field != wf.FieldInterfaceIndex || scope.Op != wf.MatchTypeEqual || condition == nil || condition.Op != wf.MatchTypeNotEqual {
		return false
	}
	index, ok := scope.Value.(uint32)
	if !ok || index == 0 {
		return false
	}
	switch r.Layer {
	case wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4:
		addr, ok := condition.Value.(netip.Addr)
		return ok && hostIPv4(addr) && (condition.Field == wf.FieldIPLocalAddress || condition.Field == wf.FieldIPRemoteAddress)
	case wf.LayerInboundTransportV4, wf.LayerOutboundTransportV4:
		switch condition.Field {
		case wf.FieldIPProtocol:
			return condition.Value == wf.IPProto(windows.IPPROTO_UDP)
		case wf.FieldIPLocalPort, wf.FieldIPRemotePort:
			return condition.Value == uint16(0)
		}
	}
	return false
}

func blockWindowsPeer(ctx context.Context, adapter *driver.Adapter) (_ *nativeFilterAPI, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := adapter.AdapterState()
	if err != nil {
		return nil, err
	}
	if state != driver.AdapterStateDown {
		return nil, ErrAdapterConflict
	}
	luid := adapter.LUID()
	row, err := luid.Interface()
	if err != nil {
		return nil, err
	}
	if luid == 0 || row == nil || row.InterfaceLUID != luid || row.InterfaceIndex == 0 {
		return nil, ErrAdapterConflict
	}
	var rules []*wf.Rule
	for _, layer := range []wf.LayerID{wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		rules = append(rules, &wf.Rule{
			Name:   "EndlessNet prepared Direct peer",
			Layer:  layer,
			Action: wf.ActionBlock, HardAction: true, Weight: 1,
			Conditions: []*wf.Match{{Field: wf.FieldInterfaceIndex, Op: wf.MatchTypeEqual, Value: row.InterfaceIndex}},
		})
	}
	api, err := installCandidateBlocks(ctx, rules)
	if err != nil {
		return nil, err
	}
	current, err := luid.Interface()
	if err != nil {
		return nil, errors.Join(err, api.close())
	}
	if current == nil || current.InterfaceLUID != luid || current.InterfaceGUID != row.InterfaceGUID || current.InterfaceIndex != row.InterfaceIndex {
		return nil, errors.Join(ErrAdapterConflict, api.close())
	}
	return api, nil
}

func installCandidateBlocks(ctx context.Context, rules []*wf.Rule) (_ *nativeFilterAPI, err error) {
	api, err := openFilterAPI(time.Second, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, api.close())
		}
	}()
	if err := beginFilterTransaction(ctx, api); err != nil {
		return nil, err
	}
	sub, err := windows.GenerateGUID()
	if err != nil {
		return nil, err
	}
	if err := api.addSublayer(sub, 0); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		id, err := windows.GenerateGUID()
		if err != nil {
			return nil, err
		}
		rule.ID, rule.Sublayer = wf.RuleID(id), wf.SublayerID(sub)
		kernelID, err := api.add(rule)
		if err != nil {
			return nil, err
		}
		readID, err := api.check(rule)
		if err != nil {
			return nil, err
		}
		if kernelID == 0 || readID != kernelID {
			return nil, ErrPolicyOwnership
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := api.commit(); err != nil {
		return nil, err
	}
	return api, nil
}
