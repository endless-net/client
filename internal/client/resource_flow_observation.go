package client

import (
	"net/netip"
	"sync"
	"time"
)

// A reply admitted by every TUN filter is evidence of one working application
// flow. The bounded in-memory sample carries no payload and is lost on restart.
const resourceFlowLifetime = 5 * time.Second
const resourceFlowLimit = 4096

type resourceFlowKey struct {
	local, remote         netip.Addr
	localPort, remotePort uint16
	protocol              byte
}

type resourceFlowSample struct {
	key        resourceFlowKey
	observed   time.Time
	generation uint64
}

type resourceFlowObserver struct {
	mu         sync.Mutex
	pending    map[resourceFlowKey]time.Time
	confirmed  map[resourceFlowKey]time.Time
	generation uint64
	lastPrune  time.Time
}

func (o *resourceFlowObserver) reset() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.pending = nil
	o.confirmed = nil
	o.generation++
	o.lastPrune = time.Time{}
	o.mu.Unlock()
}

func (o *resourceFlowObserver) observe(raw []byte, inbound bool, now time.Time) {
	if o == nil {
		return
	}
	packet, ok := parseApplicationPacket(raw)
	if !ok || packet.protocol != 6 && packet.protocol != 17 || packet.sourcePort == 0 || packet.destinationPort == 0 {
		return
	}
	key := resourceFlowKey{local: packet.source, remote: packet.destination, localPort: packet.sourcePort, remotePort: packet.destinationPort, protocol: packet.protocol}
	if inbound {
		key = resourceFlowKey{local: packet.destination, remote: packet.source, localPort: packet.destinationPort, remotePort: packet.sourcePort, protocol: packet.protocol}
	}
	if !resourceFlowPacketUsable(raw, packet.protocol, inbound) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lastPrune.IsZero() || now.Before(o.lastPrune) || now.Sub(o.lastPrune) >= time.Second {
		for key, at := range o.pending {
			if now.Before(at) || now.Sub(at) >= resourceFlowLifetime {
				delete(o.pending, key)
			}
		}
		for key, at := range o.confirmed {
			if now.Before(at) || now.Sub(at) >= resourceFlowLifetime {
				delete(o.confirmed, key)
			}
		}
		o.lastPrune = now
	}
	if !inbound {
		if len(o.pending) >= resourceFlowLimit {
			return
		}
		if o.pending == nil {
			o.pending = make(map[resourceFlowKey]time.Time)
		}
		o.pending[key] = now
		return
	}
	if at, ok := o.pending[key]; ok && !now.Before(at) && now.Sub(at) < resourceFlowLifetime {
		if len(o.confirmed) >= resourceFlowLimit {
			return
		}
		if o.confirmed == nil {
			o.confirmed = make(map[resourceFlowKey]time.Time)
		}
		o.confirmed[key] = now
		delete(o.pending, key)
	}
}

func resourceFlowPacketUsable(raw []byte, protocol byte, inbound bool) bool {
	offset := 40
	if raw[0]>>4 == 4 {
		offset = int(raw[0]&15) * 4
	}
	if protocol == 17 {
		length := int(raw[offset+4])<<8 | int(raw[offset+5])
		// A malformed or empty UDP datagram is not application response evidence.
		return length >= 8 && offset+length == len(raw) && (!inbound || length > 8)
	}
	if len(raw) < offset+20 {
		return false
	}
	header := int(raw[offset+12]>>4) * 4
	if header < 20 || offset+header > len(raw) {
		return false
	}
	flags := raw[offset+13]
	if flags&0x04 != 0 { // RST is evidence that the port is unavailable.
		return false
	}
	if inbound {
		return flags&0x10 != 0 // SYN-ACK or an established response.
	}
	return flags&0x02 != 0 || flags&0x10 != 0
}

func (o *resourceFlowObserver) samples(now time.Time) []resourceFlowSample {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]resourceFlowSample, 0, len(o.confirmed))
	for key, at := range o.confirmed {
		if !now.Before(at) && now.Sub(at) < resourceFlowLifetime {
			result = append(result, resourceFlowSample{key: key, observed: at, generation: o.generation})
		}
	}
	return result
}

func (o *resourceFlowObserver) current(sample resourceFlowSample, now time.Time) bool {
	if o == nil || now.Before(sample.observed) || now.Sub(sample.observed) >= resourceFlowLifetime {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.generation == sample.generation && o.confirmed[sample.key].Equal(sample.observed)
}
