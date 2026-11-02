//go:build windows

package dataplane

import (
	"errors"
	"math"
	"net/netip"
	"time"
	"unsafe"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

// These are the Windows SDK's 64-bit FWPM_*0 layouts, restricted to this
// owner's IPv4 ALE rules. In particular FWPM_FILTER0's context is one aligned
// 16-byte union, not adjacent rawContext/providerContextKey fields.
// https://learn.microsoft.com/en-us/windows/win32/api/fwpmtypes/ns-fwpmtypes-fwpm_filter0
type nativeDisplay struct{ Name, Description *uint16 }
type nativeBlob struct {
	Size uint32
	Data *byte
}
type nativeValue struct {
	Type  uint32
	Value uintptr
}
type nativeContext struct {
	_     [0]uint64
	Bytes [16]byte
}
type nativeAction struct {
	Type uint32
	Key  windows.GUID
}
type nativeCondition struct {
	Field windows.GUID
	Match uint32
	Value nativeValue
}
type nativeFilter struct {
	Key             windows.GUID
	Display         nativeDisplay
	Flags           uint32
	Provider        *windows.GUID
	ProviderData    nativeBlob
	Layer, Sublayer windows.GUID
	Weight          nativeValue
	Count           uint32
	Conditions      *nativeCondition
	Action          nativeAction
	Context         nativeContext
	Reserved        *windows.GUID
	ID              uint64
	EffectiveWeight nativeValue
}
type nativeSublayer struct {
	Key          windows.GUID
	Display      nativeDisplay
	Flags        uint32
	Provider     *windows.GUID
	ProviderData nativeBlob
	Weight       uint16
}
type nativeSession struct {
	Key                                 windows.GUID
	Display                             nativeDisplay
	Flags, TransactionWaitMS, ProcessID uint32
	SID                                 *windows.SID
	Username                            *uint16
	KernelMode                          uint32
}

// Native allocations keep pointers stored in Win32 union values out of the Go
// heap. Their lifetime is exactly one synchronous WFP call/comparison.
type filterArena struct{ blocks []windows.Handle }

// nativePointer reads the pointer member of a Win32 union/handle result. Values
// here are exclusively LocalAlloc/BFE addresses, never converted Go pointers.
func nativePointer(value uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&value))
}

func (a *filterArena) allocate(size uintptr) (unsafe.Pointer, error) {
	if size == 0 || size > math.MaxUint32 {
		return nil, ErrConfiguration
	}
	block, err := windows.LocalAlloc(windows.LMEM_FIXED|windows.LMEM_ZEROINIT, uint32(size))
	if err != nil || block == 0 {
		return nil, ErrPolicy
	}
	a.blocks = append(a.blocks, windows.Handle(block))
	return nativePointer(block), nil
}
func (a *filterArena) close() {
	for _, block := range a.blocks {
		_, _ = windows.LocalFree(block)
	}
	a.blocks = nil
}
func (a *filterArena) text(value string) (*uint16, error) {
	chars, err := windows.UTF16FromString(value)
	if err != nil {
		return nil, ErrConfiguration
	}
	p, err := a.allocate(uintptr(len(chars)) * 2)
	if err != nil {
		return nil, err
	}
	copy(unsafe.Slice((*uint16)(p), len(chars)), chars)
	return (*uint16)(p), nil
}
func (a *filterArena) number(value uint64) (nativeValue, error) {
	p, err := a.allocate(8)
	if err != nil {
		return nativeValue{}, err
	}
	*(*uint64)(p) = value
	return nativeValue{Type: 4, Value: uintptr(p)}, nil
}

type nativeFilterAPI struct {
	handle windows.Handle
	procs  map[string]*windows.LazyProc
}

func classifyPolicyError(err error) error {
	if errors.Is(err, ErrPolicyOwnership) || errors.Is(err, windows.Errno(windows.FWP_E_FILTER_NOT_FOUND)) || errors.Is(err, windows.Errno(windows.FWP_E_SUBLAYER_NOT_FOUND)) {
		return ErrPolicyOwnership
	}
	if errors.Is(err, ErrPolicyPermission) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return ErrPolicyPermission
	}
	return ErrPolicy
}

func openFilterAPI(wait time.Duration, dynamic bool) (*nativeFilterAPI, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 || wait <= 0 || wait%time.Millisecond != 0 || wait/time.Millisecond > math.MaxUint32 {
		return nil, ErrConfiguration
	}
	dll := windows.NewLazySystemDLL("fwpuclnt.dll")
	api := &nativeFilterAPI{procs: make(map[string]*windows.LazyProc)}
	for _, name := range []string{"FwpmEngineOpen0", "FwpmEngineClose0", "FwpmTransactionBegin0", "FwpmTransactionCommit0", "FwpmTransactionAbort0", "FwpmSubLayerAdd0", "FwpmSubLayerGetByKey0", "FwpmSubLayerDeleteByKey0", "FwpmFilterAdd0", "FwpmFilterGetByKey0", "FwpmFilterDeleteByKey0", "FwpmFreeMemory0"} {
		p := dll.NewProc(name)
		if p.Find() != nil {
			return nil, ErrPolicy
		}
		api.procs[name] = p
	}
	var arena filterArena
	defer arena.close()
	name, err := arena.text("EndlessNet device filters")
	if err != nil {
		return nil, err
	}
	session := nativeSession{Display: nativeDisplay{Name: name}, TransactionWaitMS: uint32(wait / time.Millisecond)}
	if dynamic {
		session.Flags = 1
	}
	if err := api.call("FwpmEngineOpen0", 0, 10, 0, uintptr(unsafe.Pointer(&session)), uintptr(unsafe.Pointer(&api.handle))); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, ErrPolicyPermission
		}
		return nil, ErrPolicy
	}
	if api.handle == 0 {
		return nil, ErrPolicy
	}
	return api, nil
}

// Pointer arguments must escape and remain live through this wrapper, just as
// for windows.LazyProc.Call itself; a uintptr must not retain a movable Go stack
// address across an intermediate Go call.
//
//go:uintptrescapes
func (a *nativeFilterAPI) call(name string, args ...uintptr) error {
	status, _, _ := a.procs[name].Call(args...)
	if status != 0 {
		return windows.Errno(status)
	}
	return nil
}
func (a *nativeFilterAPI) begin() error  { return a.call("FwpmTransactionBegin0", uintptr(a.handle), 0) }
func (a *nativeFilterAPI) commit() error { return a.call("FwpmTransactionCommit0", uintptr(a.handle)) }
func (a *nativeFilterAPI) abort() error  { return a.call("FwpmTransactionAbort0", uintptr(a.handle)) }
func (a *nativeFilterAPI) close() error {
	if err := a.call("FwpmEngineClose0", uintptr(a.handle)); err != nil {
		return err
	}
	a.handle = 0
	return nil
}
func (a *nativeFilterAPI) sublayer(key windows.GUID) error {
	var arena filterArena
	defer arena.close()
	name, err := arena.text("EndlessNet device policy")
	if err != nil {
		return err
	}
	// Relative priority within this owner's sublayer is defined by rule
	// weights. Soft permits preserve firewall vetoes in other sublayers.
	sub := nativeSublayer{Key: key, Display: nativeDisplay{Name: name}, Flags: 1}
	return a.call("FwpmSubLayerAdd0", uintptr(a.handle), uintptr(unsafe.Pointer(&sub)), 0)
}
func (a *nativeFilterAPI) checkSublayer(key windows.GUID) error {
	var actual *nativeSublayer
	if err := a.call("FwpmSubLayerGetByKey0", uintptr(a.handle), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&actual))); err != nil {
		return err
	}
	defer func() { _, _, _ = a.procs["FwpmFreeMemory0"].Call(uintptr(unsafe.Pointer(&actual))) }()
	var arena filterArena
	defer arena.close()
	name, err := arena.text("EndlessNet device policy")
	if err != nil {
		return err
	}
	if actual == nil || actual.Key != key || actual.Flags != 1 || actual.Provider != nil || actual.ProviderData.Size != 0 || actual.Weight != 0 || !sameNativeText(actual.Display.Name, name, 128) || !emptyNativeText(actual.Display.Description) {
		return ErrPolicyOwnership
	}
	return nil
}
func (a *nativeFilterAPI) removeSublayer(key windows.GUID) error {
	return a.call("FwpmSubLayerDeleteByKey0", uintptr(a.handle), uintptr(unsafe.Pointer(&key)))
}
func marshalNativeFilter(arena *filterArena, rule *wf.Rule) (*nativeFilter, error) {
	if !supportedNativeRule(rule) {
		return nil, ErrConfiguration
	}
	p, err := arena.allocate(unsafe.Sizeof(nativeFilter{}))
	if err != nil {
		return nil, err
	}
	f := (*nativeFilter)(p)
	f.Key, f.Layer, f.Sublayer = windows.GUID(rule.ID), windows.GUID(rule.Layer), windows.GUID(rule.Sublayer)
	f.Action.Type = uint32(rule.Action)
	if rule.HardAction {
		f.Flags = 8
	}
	if rule.Persistent {
		f.Flags |= 1
	}
	f.Display.Name, err = arena.text(rule.Name)
	if err != nil {
		return nil, err
	}
	f.Weight, err = arena.number(rule.Weight)
	if err != nil {
		return nil, err
	}
	p, err = arena.allocate(uintptr(len(rule.Conditions)) * unsafe.Sizeof(nativeCondition{}))
	if err != nil {
		return nil, err
	}
	f.Conditions, f.Count = (*nativeCondition)(p), uint32(len(rule.Conditions))
	for i, match := range rule.Conditions {
		if match == nil {
			return nil, ErrConfiguration
		}
		c := &unsafe.Slice(f.Conditions, len(rule.Conditions))[i]
		c.Field, c.Match = windows.GUID(match.Field), uint32(match.Op)
		switch value := match.Value.(type) {
		case uint64:
			c.Value, err = arena.number(value)
		case uint16:
			c.Value = nativeValue{Type: 2, Value: uintptr(value)}
		case uint32:
			c.Value = nativeValue{Type: 3, Value: uintptr(value)}
		case wf.IPProto:
			c.Value = nativeValue{Type: 1, Value: uintptr(value)}
		case wf.ConditionFlag:
			c.Value = nativeValue{Type: 3, Value: uintptr(value)}
		case netip.Addr:
			if !value.Is4() {
				return nil, ErrConfiguration
			}
			c.Value = nativeValue{Type: 3, Value: uintptr(ipv4Number(value))}
		default:
			return nil, ErrConfiguration
		}
		if err != nil {
			return nil, err
		}
	}
	return f, nil
}

// This domain binding supports our ALE and IPv4 packet layers only.
// Reject unsupported properties instead of silently omitting them from Windows.
func supportedNativeRule(r *wf.Rule) bool {
	if r == nil || r.ID.IsZero() || r.Sublayer.IsZero() || r.KernelID != 0 || r.Name == "" || len(r.Name) > 128 || r.Description != "" || r.Provider != (wf.ProviderID{}) || len(r.ProviderData) != 0 || r.Callout != (wf.CalloutID{}) || r.BootTime || r.Disabled || r.PermitIfMissing {
		return false
	}
	if r.Layer == wf.LayerInboundIPPacketV4 || r.Layer == wf.LayerOutboundIPPacketV4 {
		return supportedPacketGuard(r)
	}
	if r.Persistent || r.Layer != wf.LayerALEAuthConnectV4 && r.Layer != wf.LayerALEAuthRecvAcceptV4 {
		return false
	}
	switch r.Action {
	case wf.ActionBlock:
		if !r.HardAction || r.Weight != 1 || len(r.Conditions) != 1 {
			return false
		}
	case wf.ActionPermit:
		if r.HardAction || r.Weight != 2 || len(r.Conditions) < 5 || len(r.Conditions) > 6 {
			return false
		}
	default:
		return false
	}
	seen := make(map[wf.FieldID]bool, len(r.Conditions))
	for _, c := range r.Conditions {
		if c == nil || seen[c.Field] {
			return false
		}
		seen[c.Field] = true
		if c.Field == wf.FieldFlags {
			value, ok := c.Value.(wf.ConditionFlag)
			if !ok || value != wf.ConditionFlagIsReauthorize || c.Op != wf.MatchTypeFlagsAllSet {
				return false
			}
			continue
		}
		if c.Op != wf.MatchTypeEqual {
			return false
		}
		switch c.Field {
		case wf.FieldIPLocalInterface:
			value, ok := c.Value.(uint64)
			if !ok || value == 0 {
				return false
			}
		case wf.FieldIPLocalAddress, wf.FieldIPRemoteAddress:
			value, ok := c.Value.(netip.Addr)
			if !ok || !value.Is4() {
				return false
			}
		case wf.FieldIPProtocol:
			value, ok := c.Value.(wf.IPProto)
			if !ok || value != 6 && value != 17 {
				return false
			}
		case wf.FieldIPLocalPort, wf.FieldIPRemotePort:
			value, ok := c.Value.(uint16)
			if !ok || value == 0 {
				return false
			}
		default:
			return false
		}
	}
	if !seen[wf.FieldIPLocalInterface] {
		return false
	}
	if r.Action == wf.ActionBlock {
		return true
	}
	port := wf.FieldIPRemotePort
	if r.Layer == wf.LayerALEAuthRecvAcceptV4 {
		port = wf.FieldIPLocalPort
	}
	return seen[wf.FieldIPLocalAddress] && seen[wf.FieldIPRemoteAddress] && seen[wf.FieldIPProtocol] && seen[port] && (len(seen) == 5 || len(seen) == 6 && seen[wf.FieldFlags])
}

func supportedPacketGuard(r *wf.Rule) bool {
	switch r.Action {
	case wf.ActionBlock:
		if !r.Persistent || !r.HardAction || r.Weight != 1 || len(r.Conditions) != 1 {
			return false
		}
	case wf.ActionPermit:
		if r.Persistent || r.HardAction || r.Weight != 2 || len(r.Conditions) != 3 {
			return false
		}
	default:
		return false
	}
	seen := make(map[wf.FieldID]bool, len(r.Conditions))
	scopes := 0
	for _, c := range r.Conditions {
		if c == nil || c.Op != wf.MatchTypeEqual || seen[c.Field] {
			return false
		}
		seen[c.Field] = true
		switch c.Field {
		case wf.FieldIPLocalAddress, wf.FieldIPRemoteAddress:
			value, ok := c.Value.(netip.Addr)
			if !ok || !hostIPv4(value) {
				return false
			}
			scopes++
		case wf.FieldIPLocalInterface:
			value, ok := c.Value.(uint64)
			if !ok || value == 0 {
				return false
			}
		case wf.FieldInterfaceIndex:
			value, ok := c.Value.(uint32)
			if !ok || value == 0 {
				return false
			}
		default:
			return false
		}
	}
	return scopes == 1 && (r.Action == wf.ActionBlock || seen[wf.FieldIPLocalInterface] && seen[wf.FieldInterfaceIndex])
}
func (a *nativeFilterAPI) add(rule *wf.Rule) (uint64, error) {
	var arena filterArena
	defer arena.close()
	f, err := marshalNativeFilter(&arena, rule)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = a.call("FwpmFilterAdd0", uintptr(a.handle), uintptr(unsafe.Pointer(f)), 0, uintptr(unsafe.Pointer(&id)))
	return id, err
}
func (a *nativeFilterAPI) remove(key wf.RuleID) error {
	return a.call("FwpmFilterDeleteByKey0", uintptr(a.handle), uintptr(unsafe.Pointer(&key)))
}
func (a *nativeFilterAPI) check(rule *wf.Rule) (uint64, error) {
	var actual *nativeFilter
	key := windows.GUID(rule.ID)
	if err := a.call("FwpmFilterGetByKey0", uintptr(a.handle), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&actual))); err != nil {
		return 0, err
	}
	defer func() { _, _, _ = a.procs["FwpmFreeMemory0"].Call(uintptr(unsafe.Pointer(&actual))) }()
	var arena filterArena
	defer arena.close()
	expected, err := marshalNativeFilter(&arena, rule)
	if err != nil || !sameNativeFilter(actual, expected) {
		return 0, ErrPolicyOwnership
	}
	return actual.ID, nil
}
func equalNativeValue(a, b nativeValue) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case 1:
		return uint8(a.Value) == uint8(b.Value)
	case 2:
		return uint16(a.Value) == uint16(b.Value)
	case 3:
		return uint32(a.Value) == uint32(b.Value)
	case 4:
		return a.Value != 0 && b.Value != 0 && *(*uint64)(nativePointer(a.Value)) == *(*uint64)(nativePointer(b.Value))
	}
	return false
}
func sameNativeFilter(a, b *nativeFilter) bool {
	if a == nil || a.Key != b.Key || a.Layer != b.Layer || a.Sublayer != b.Sublayer || a.Flags != b.Flags || a.Provider != nil || a.ProviderData.Size != 0 || a.Action != b.Action || a.Context != b.Context || a.Reserved != nil || a.Count != b.Count || a.Count > 8 || a.Conditions == nil || !equalNativeValue(a.Weight, b.Weight) {
		return false
	}
	// Names are immutable ownership metadata, not authority or credentials.
	if !sameNativeText(a.Display.Name, b.Display.Name, 128) || !emptyNativeText(a.Display.Description) {
		return false
	}
	actual := make(map[windows.GUID]nativeCondition, a.Count)
	for _, c := range unsafe.Slice(a.Conditions, int(a.Count)) {
		if _, exists := actual[c.Field]; exists {
			return false
		}
		actual[c.Field] = c
	}
	for _, c := range unsafe.Slice(b.Conditions, int(b.Count)) {
		other, exists := actual[c.Field]
		if !exists || other.Match != c.Match || !equalNativeValue(other.Value, c.Value) {
			return false
		}
	}
	return true
}
func emptyNativeText(p *uint16) bool { return p == nil || *p == 0 }
func sameNativeText(a, b *uint16, limit int) bool {
	if a == nil || b == nil {
		return a == b
	}
	for i := 0; i <= limit; i++ {
		av := *(*uint16)(unsafe.Add(unsafe.Pointer(a), i*2))
		bv := *(*uint16)(unsafe.Add(unsafe.Pointer(b), i*2))
		if av != bv {
			return false
		}
		if av == 0 {
			return true
		}
	}
	return false
}
