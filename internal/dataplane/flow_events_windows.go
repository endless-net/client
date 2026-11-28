//go:build windows

package dataplane

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

type wfpDecision struct {
	layer          wf.LayerID
	ingress, allow bool
}

type wfpEventScope struct {
	rules map[uint64]wfpDecision
	done  chan struct{}
}

// Called under WindowsFilters.mu before changing any owned filters.
func (f *WindowsFilters) endEventScope() {
	if f.eventScope != nil {
		close(f.eventScope.done)
		f.eventScope = nil
	}
}

// FWPM_NET_EVENT2, including the naturally aligned FWPM_NET_EVENT_HEADER2.
// The classifier structures share filterId as their first member. Optional
// identity and application pointers are never followed.
type wfpClassifyEvent struct {
	stamp                 windows.Filetime
	flags, version        uint32
	protocol              uint8
	_                     [3]byte
	local, remote         [4]uint32
	localPort, remotePort uint16
	_                     uint32     // scope
	_                     nativeBlob // app
	_                     uintptr    // user
	_                     uint32     // family
	_                     uintptr    // pkg
	kind                  uint32
	classify              *struct {
		filterID uint64
		layerID  uint16
	}
}

type wfpEventSubscription struct {
	template uintptr
	flags    uint32
	session  windows.GUID
}

type wfpFlowObserver struct {
	monitor *connectivity.FlowMonitor
	value   connectivity.Map
	capture *connectivity.FilterInterval
	scope   *wfpEventScope
	layers  map[wf.LayerID]uint16
}

func wfpIPv4(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func (o *wfpFlowObserver) record(e *wfpClassifyEvent) {
	select {
	case <-o.scope.done:
		return
	case <-o.capture.Done():
		return
	default:
	}
	if e == nil || (e.kind != 3 && e.kind != 6) || e.classify == nil {
		return
	}
	rule, owned := o.scope.rules[e.classify.filterID]
	if !owned || o.layers[rule.layer] != e.classify.layerID || rule.allow != (e.kind == 6) || e.version != 0 {
		return
	}
	// IP_VERSION, IP_PROTOCOL, both addresses and the destination port.
	flags, port := uint32(0x107|0x10), e.remotePort
	if rule.ingress {
		flags, port = 0x107|0x8, e.localPort
	}
	if e.flags&flags != flags {
		return
	}
	var protocol string
	switch e.protocol {
	case 6:
		protocol = "tcp"
	case 17:
		protocol = "udp"
	default:
		return
	}
	o.monitor.ObserveClassification(o.capture, o.value, time.Unix(0, e.stamp.Nanoseconds()), wfpIPv4(e.local[0]), wfpIPv4(e.remote[0]), port, protocol, rule.ingress, rule.allow)
}

var (
	wfpObservers     sync.Map
	wfpObserverID    atomic.Uint64
	eventDLL         = windows.NewLazySystemDLL("fwpuclnt.dll")
	subscribeWFP     = eventDLL.NewProc("FwpmNetEventSubscribe1")
	unsubscribeWFP   = eventDLL.NewProc("FwpmNetEventUnsubscribe0")
	wfpEventCallback = windows.NewCallback(func(id uintptr, event *wfpClassifyEvent) uintptr {
		if v, ok := wfpObservers.Load(id); ok {
			v.(*wfpFlowObserver).record(event)
		}
		return 0
	})
)

func (s *WindowsSession) observeWFP() {
	for s.lifetime.Err() == nil {
		if s.flows.Collecting() {
			s.mu.Lock()
			m := s.current
			s.filters.mu.Lock()
			scope := s.filters.eventScope
			s.filters.mu.Unlock()
			s.mu.Unlock()
			if scope != nil {
				if capture := s.flows.BeginFilterInterval(m); capture != nil {
					o := wfpFlowObserver{monitor: &s.flows, value: m, capture: capture, scope: scope}
					o.listen(s)
				}
			}
		}
		select {
		case <-s.lifetime.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (o *wfpFlowObserver) listen(s *WindowsSession) {
	if subscribeWFP.Find() != nil || unsubscribeWFP.Find() != nil {
		return
	}
	engine, err := openFilterAPI(s.config.TransactionWait, true)
	if err != nil {
		return
	}
	defer func() { _ = engine.close() }()
	o.layers = make(map[wf.LayerID]uint16)
	for _, key := range []wf.LayerID{wf.LayerALEAuthConnectV4, wf.LayerALEAuthRecvAcceptV4, wf.LayerInboundIPPacketV4, wf.LayerOutboundIPPacketV4} {
		id, ok := eventLayerID(engine, key)
		if !ok {
			return
		}
		o.layers[key] = id
	}
	id := uintptr(wfpObserverID.Add(1))
	wfpObservers.Store(id, o)
	defer wfpObservers.Delete(id)
	var sub wfpEventSubscription
	var handle windows.Handle
	code, _, _ := subscribeWFP.Call(uintptr(engine.handle), uintptr(unsafe.Pointer(&sub)), wfpEventCallback, id, uintptr(unsafe.Pointer(&handle)))
	if code != 0 {
		return
	}
	select {
	case <-o.capture.Done():
	case <-o.scope.done:
	case <-s.lifetime.Done():
	}
	_, _, _ = unsubscribeWFP.Call(uintptr(engine.handle), uintptr(handle))
}

// FWPM_LAYER0 carries the runtime layer ID reported in classify events.
func eventLayerID(engine *nativeFilterAPI, key wf.LayerID) (uint16, bool) {
	type layer struct {
		key  windows.GUID
		_    nativeDisplay // display
		_, _ uint32        // flags, count
		_    uintptr       // fields
		_    windows.GUID  // sublayer
		id   uint16
	}
	var data *layer
	proc := eventDLL.NewProc("FwpmLayerGetByKey0")
	if proc.Find() != nil {
		return 0, false
	}
	code, _, _ := proc.Call(uintptr(engine.handle), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&data)))
	if code != 0 || data == nil {
		return 0, false
	}
	defer func() { _, _, _ = engine.procs["FwpmFreeMemory0"].Call(uintptr(unsafe.Pointer(&data))) }()
	return data.id, data.key == windows.GUID(key)
}
