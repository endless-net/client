package client

import (
	"errors"
	"net/netip"
	"slices"
	"sync"
	"time"
)

type resourceDenyRule struct {
	prefix   netip.Prefix
	protocol byte
	port     uint16
}

// Denials are intersected with all other packet authorization. In particular,
// an enabled narrower resource cannot reopen a disabled overlapping prefix.
type resourcePacketFilter struct {
	mu                       sync.RWMutex
	current, pending         []resourceDenyRule
	expires, pendingExpires  time.Time
	applying, closed, active bool
}

func (f *resourcePacketFilter) differs(rules []resourceDenyRule) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return !f.active || f.closed || !slices.Equal(f.current, rules)
}

func (f *resourcePacketFilter) hasAuthority() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.active
}

func (f *resourcePacketFilter) suspend(rules []resourceDenyRule, expires time.Time) error {
	if len(rules) > 8192 || expires.IsZero() {
		return errors.New("invalid resource filter bounds or expiry")
	}
	for _, rule := range rules {
		if !rule.prefix.IsValid() || rule.prefix.Bits() == 0 || rule.prefix != rule.prefix.Masked() || (rule.protocol != 0 && rule.protocol != 6 && rule.protocol != 17) || (rule.protocol == 0) != (rule.port == 0) {
			return errors.New("invalid resource packet rule")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending, f.applying, f.active = slices.Clone(rules), true, true
	f.pendingExpires = expires
	// Keep the old deadline until commit as well as the old denials.
	if f.expires.IsZero() || expires.Before(f.expires) {
		f.expires = expires
	}
	return nil
}

func (f *resourcePacketFilter) commit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.applying {
		return
	}
	f.current, f.pending = f.pending, nil
	f.applying, f.closed, f.expires = false, false, f.pendingExpires
}

func (f *resourcePacketFilter) withdraw() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active {
		f.closed = true
	}
}

func (f *resourcePacketFilter) allows(raw []byte, inbound bool, now time.Time) bool {
	if f == nil {
		return true
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.closed || f.active && !now.Before(f.expires) {
		return false
	}
	if !f.active {
		return true
	}
	packet, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	address, port := packet.destination, packet.destinationPort
	if inbound {
		address, port = packet.source, packet.sourcePort
	}
	denied := func(rules []resourceDenyRule) bool {
		for _, rule := range rules {
			if rule.prefix.Contains(address) && (rule.protocol == 0 || rule.protocol == packet.protocol && rule.port == port) {
				return true
			}
		}
		return false
	}
	return !denied(f.current) && (!f.applying || !denied(f.pending))
}
