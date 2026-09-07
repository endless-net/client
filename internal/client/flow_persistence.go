package client

import (
	"errors"
	"time"

	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"google.golang.org/protobuf/proto"
)

// flowPersistence is owned by the worker goroutine. Loaded windows stay in
// quarantine until a fresh policy response authorizes that exact revision.
type flowPersistence struct {
	spool       *flowSpool
	version     uint64
	expires     time.Time
	quarantine  []*clientrpc.FlowWindow
	ready       bool
	lastVersion uint64
	lastExpires time.Time
	lastWindows []*clientrpc.FlowWindow
	saved       bool
}

func (e *WireGuardEngine) discardFlowSpoolLocked() {
	if e.opts.FlowSpoolPath == "" {
		return
	}
	if err := (&flowSpool{path: e.opts.FlowSpoolPath}).discard(); err != nil && e.flows != nil {
		e.flows.mu.Lock()
		e.flows.storageFailures++
		e.flows.mu.Unlock()
	}
}

func openFlowPersistence(spool *flowSpool, c *flowCollector, now time.Time) (*flowPersistence, error) {
	p := &flowPersistence{spool: spool}
	if spool == nil {
		return p, nil
	}
	var err error
	p.version, p.expires, p.quarantine, err = spool.load(now)
	if err != nil {
		c.mu.Lock()
		c.storageFailures++
		if errors.Is(err, errInvalidFlowSpool) {
			c.corruptSpools++
		}
		c.mu.Unlock()
		if discardErr := spool.discard(); discardErr != nil {
			return nil, discardErr
		}
	}
	return p, nil
}

func (p *flowPersistence) discardQuarantine(c *flowCollector) {
	c.mu.Lock()
	c.droppedWindows += uint64(len(p.quarantine))
	c.mu.Unlock()
	p.quarantine = nil
}

func (p *flowPersistence) acceptPolicy(c *flowCollector, now time.Time) {
	p.ready = true
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, window := range p.quarantine {
		if p.version != c.version || !now.Before(p.expires) || window.GetWindowStart().AsTime().Before(c.notBefore) || len(c.active)+len(c.pending) >= maxFlowWindows {
			c.droppedWindows++
			continue
		}
		c.pending = append(c.pending, window)
		c.restoredWindows++
	}
	p.quarantine = nil
}

func (p *flowPersistence) checkpoint(c *flowCollector, now time.Time) error {
	if p.spool == nil {
		return nil
	}
	if !p.ready {
		if len(p.quarantine) > 0 && !now.Before(p.expires) {
			p.discardQuarantine(c)
			return p.spool.discard()
		}
		return nil
	}
	c.mu.Lock()
	if c.version != 0 && !now.Before(c.expires) {
		c.clearLocked()
	}
	version, expires := c.version, c.expires
	windows := make([]*clientrpc.FlowWindow, 0, len(c.pending))
	for _, window := range c.pending {
		windows = append(windows, proto.Clone(window).(*clientrpc.FlowWindow))
	}
	c.mu.Unlock()
	unchanged := p.saved && version == p.lastVersion && expires.Equal(p.lastExpires) && len(windows) == len(p.lastWindows)
	if unchanged {
		for i, window := range windows {
			if !proto.Equal(window, p.lastWindows[i]) {
				unchanged = false
				break
			}
		}
	}
	if unchanged {
		return nil
	}
	err := p.spool.save(version, expires, windows)
	if err != nil {
		c.mu.Lock()
		c.storageFailures++
		c.mu.Unlock()
	} else {
		p.saved = true
		p.lastVersion = version
		p.lastExpires = expires
		p.lastWindows = windows
	}
	return err
}

func (p *flowPersistence) purge(c *flowCollector) error {
	p.ready = true
	p.discardQuarantine(c)
	c.stop()
	return p.checkpoint(c, time.Now())
}
