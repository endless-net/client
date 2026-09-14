package client

import (
	"encoding/binary"
	"sync"
	"time"
)

const inboundFlowLimit = 4096

type inboundFlow struct {
	until    time.Time
	tcpState byte
}

// This filter only narrows traffic already accepted by the peer/application/
// sharing filters. Flow state is volatile and never survives a policy change.
type inboundPacketFilter struct {
	mu      sync.Mutex
	blocked bool
	flows   map[applicationPacket]inboundFlow
	binding string
}

func (f *inboundPacketFilter) suspend(allowed bool, binding string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := f.blocked == allowed || f.binding != binding
	if f.binding != binding {
		f.flows = nil
	}
	f.binding = binding
	// Never enable new inbound traffic before the matching runtime commits.
	f.blocked = f.blocked || !allowed
	return changed
}

func (f *inboundPacketFilter) setAllowed(allowed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked != !allowed {
		f.flows = nil
	}
	f.blocked = !allowed
}

func (f *inboundPacketFilter) allows(raw []byte, inbound bool, now time.Time) bool {
	if f == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.blocked {
		return true
	}
	p, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	offset, end := 40, 40+int(binary.BigEndian.Uint16(raw[4:6]))
	if raw[0]>>4 == 4 {
		offset, end = int(raw[0]&15)*4, int(binary.BigEndian.Uint16(raw[2:4]))
	}
	key := p
	if inbound {
		key.source, key.destination, key.sourcePort, key.destinationPort = p.destination, p.source, p.destinationPort, p.sourcePort
	}
	if p.protocol == 1 || p.protocol == 58 {
		if offset+8 > end || raw[offset+1] != 0 {
			return false
		}
		requestType, replyType := byte(8), byte(0)
		if p.protocol == 58 {
			requestType, replyType = 128, 129
		}
		if inbound && raw[offset] != replyType || !inbound && raw[offset] != requestType {
			return false
		}
		key.sourcePort, key.destinationPort = binary.BigEndian.Uint16(raw[offset+4:offset+6]), binary.BigEndian.Uint16(raw[offset+6:offset+8])
	} else if p.protocol != 6 && p.protocol != 17 {
		return !inbound
	}
	if p.protocol == 17 {
		length := int(binary.BigEndian.Uint16(raw[offset+4 : offset+6]))
		if length < 8 || offset+length > end {
			return false
		}
	}
	flow, exists := f.flows[key]
	exists = exists && now.Before(flow.until)
	if !exists {
		flow = inboundFlow{}
	}
	if p.protocol == 6 {
		header := int(raw[offset+12]>>4) * 4
		if header < 20 || offset+header > end {
			return false
		}
		flags := raw[offset+13] & 0x3f
		switch {
		case !inbound && flags == 2:
			flow.tcpState = 1
		case !exists:
			return false
		case inbound && flags == 18:
			if flow.tcpState > 2 {
				return false
			}
			flow.tcpState = 2
		case flags&2 != 0:
			return false
		case flags&4 != 0:
			delete(f.flows, key)
			return true
		default:
			if flags&16 == 0 || !inbound && flow.tcpState < 2 || inbound && flow.tcpState < 3 {
				return false
			}
			flow.tcpState = 3
		}
	} else if inbound && !exists {
		return false
	}
	if !exists && len(f.flows) >= inboundFlowLimit {
		for key, value := range f.flows {
			if !now.Before(value.until) {
				delete(f.flows, key)
			}
		}
		if len(f.flows) >= inboundFlowLimit {
			return false
		}
	}
	if f.flows == nil {
		f.flows = make(map[applicationPacket]inboundFlow)
	}
	idle := 30 * time.Second
	if p.protocol == 6 {
		idle = 2 * time.Minute
	}
	// An unsolicited reply cannot extend the UDP/ICMP response window.
	if !inbound || p.protocol == 6 {
		flow.until = now.Add(idle)
	}
	f.flows[key] = flow
	return true
}
