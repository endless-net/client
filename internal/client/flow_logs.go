package client

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"

	coordinatorapi "github.com/endless-net/coordinator/coordinatorapi/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxFlowWindows = 1024

type flowKey struct {
	source, destination, protocol, decision string
	port                                    uint32
}
type flowCollector struct {
	mu                                                                  sync.Mutex
	version                                                             uint64
	notBefore, expires                                                  time.Time
	active                                                              map[flowKey]*coordinatorapi.FlowWindow
	pending                                                             []*coordinatorapi.FlowWindow
	droppedPackets                                                      uint64
	droppedWindows                                                      uint64
	unsupportedPackets, capacityDrops, clockDrops                       uint64
	reportAttempts, acknowledgedWindows, reportFailures, policyFailures uint64
}

func (c *flowCollector) clearLocked() {
	c.droppedWindows += uint64(len(c.active) + len(c.pending))
	c.active = nil
	c.pending = nil
	c.version = 0
	c.expires = time.Time{}
}
func (c *flowCollector) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearLocked()
}

func (c *flowCollector) policy(policy *coordinatorapi.GetFlowLogPolicyResponse, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if policy.GetConsentVersion() == 0 || policy.GetCollectionNotBefore() == nil || !policy.GetCollectionNotBefore().IsValid() || policy.GetCollectionExpiresAt() == nil || !policy.GetCollectionExpiresAt().IsValid() || !policy.GetCollectionExpiresAt().AsTime().After(now) || policy.GetCollectionExpiresAt().AsTime().After(now.Add(time.Minute)) {
		c.clearLocked()
		return
	}
	if c.version != policy.GetConsentVersion() || !now.Before(c.expires) {
		c.clearLocked()
	}
	c.version = policy.GetConsentVersion()
	c.notBefore = policy.GetCollectionNotBefore().AsTime()
	c.expires = policy.GetCollectionExpiresAt().AsTime()
}

func (c *flowCollector) observe(raw []byte, allowed bool, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == 0 {
		return
	}
	if !now.Before(c.expires) {
		c.clearLocked()
		return
	}
	if now.Before(c.notBefore) {
		return
	}
	packet, ok := parseApplicationPacket(raw)
	if !ok {
		c.droppedPackets++
		c.unsupportedPackets++
		return
	}
	protocol := "other"
	switch packet.protocol {
	case 6:
		protocol = "tcp"
	case 17:
		protocol = "udp"
	case 1:
		protocol = "icmp"
	case 58:
		protocol = "icmpv6"
	}
	decision := "observed"
	if !allowed {
		decision = "deny"
	}
	key := flowKey{source: packet.source.String(), destination: packet.destination.String(), protocol: protocol, decision: decision, port: uint32(packet.destinationPort)}
	window := c.active[key]
	if window != nil && now.Before(window.GetWindowStart().AsTime()) {
		c.droppedPackets++
		c.clockDrops++
		return
	}
	if window != nil && now.Sub(window.GetWindowStart().AsTime()) >= 10*time.Second {
		c.pending = append(c.pending, window)
		delete(c.active, key)
		window = nil
	}
	if window == nil {
		if len(c.active)+len(c.pending) >= maxFlowWindows {
			c.droppedPackets++
			c.capacityDrops++
			return
		}
		window = &coordinatorapi.FlowWindow{WindowId: rand.Text(), Source: key.source, Destination: key.destination, DestinationPort: key.port, Protocol: key.protocol, Decision: key.decision, WindowStart: timestamppb.New(now)}
		if c.active == nil {
			c.active = make(map[flowKey]*coordinatorapi.FlowWindow)
		}
		c.active[key] = window
	}
	size := uint64(binary.BigEndian.Uint16(raw[2:4]))
	if raw[0]>>4 == 6 {
		size = 40 + uint64(binary.BigEndian.Uint16(raw[4:6]))
	}
	window.Bytes += size
	window.Packets++
	window.WindowEnd = timestamppb.New(now)
}

func (c *flowCollector) next(now time.Time) (*coordinatorapi.FlowWindow, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !now.Before(c.expires) {
		c.clearLocked()
		return nil, 0
	}
	for key, window := range c.active {
		if now.Sub(window.GetWindowStart().AsTime()) >= 10*time.Second {
			c.pending = append(c.pending, window)
			delete(c.active, key)
		}
	}
	if len(c.pending) == 0 {
		return nil, c.version
	}
	return proto.Clone(c.pending[0]).(*coordinatorapi.FlowWindow), c.version
}
func (c *flowCollector) acknowledge(id string, version uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == version && len(c.pending) > 0 && c.pending[0].GetWindowId() == id {
		c.acknowledgedWindows++
		c.pending[0] = nil
		c.pending = c.pending[1:]
	}
}
