package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

// A peer challenge grants a bounded echo opportunity, never own proof.
// Its receipt survives duplicate plaintext and retains the original deadline.
type serviceReceipt struct {
	window     *probeCycle
	attempt    *probeAttempt
	assignment candidateAssignment
	request    transport.Control
}

func (c *assignedCipher) controlSnapshot() (candidateAssignment, *pathChallenge, bool) {
	c.mu.Lock()
	p, closed := c.probe, c.closed
	c.mu.Unlock()
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	return a, p, closed
}

func controlKey(value transport.Control) [32]byte {
	raw, _ := transport.MarshalControl(value)
	return sha256.Sum256(raw)
}

// Called with meterMu held. A new nonce/CID/map cannot renew the window. Only
// expiration of the original 30 seconds starts another maintenance window.
// Clock faults and owner cancellation are terminal for the existing window.
func (c *assignedCipher) serviceCycleLocked() (*probeCycle, error) {
	if c.serviceWindow != nil {
		if err := c.serviceWindow.check(); err == nil {
			return c.serviceWindow, nil
		} else if !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if _, err := c.serviceWindow.timer.Remaining(); !errors.Is(err, context.DeadlineExceeded) {
			if err == nil {
				return nil, context.DeadlineExceeded
			}
			return nil, err
		}
	}
	window, err := newProbeCycle(c.bind.ctx, c.clock, c.profileID, 1, "service-window")
	if err != nil {
		return nil, err
	}
	c.serviceWindow, c.serviceReceipts = window, make(map[[32]byte]*serviceReceipt)
	return window, nil
}

func (c *assignedCipher) rememberChallenge(a candidateAssignment, value transport.Control) (*serviceReceipt, error) {
	c.meterMu.Lock()
	defer c.meterMu.Unlock()
	window, err := c.serviceCycleLocked()
	if err != nil {
		return nil, err
	}
	key := controlKey(replyControl(value, transport.Echo))
	if receipt := c.serviceReceipts[key]; receipt != nil {
		return receipt, receipt.attempt.check()
	}
	attempt, err := window.attempt(hex.EncodeToString(key[:]))
	if err != nil {
		return nil, err
	}
	receipt := &serviceReceipt{window: window, attempt: attempt, assignment: a, request: value}
	c.serviceReceipts[key] = receipt
	return receipt, nil
}

func (c *assignedCipher) receiptCurrent(receipt *serviceReceipt) error {
	if receipt == nil {
		return ErrNoAuthority
	}
	a, _, closed := c.controlSnapshot()
	if closed || !receipt.assignment.sameProbe(a) {
		return ErrNoAuthority
	}
	if err := c.bind.controlBindingAllowed(receipt.request, true); err != nil {
		return err
	}
	return receipt.attempt.check()
}

// The raw bind already meters every cold packet, including prelude/handshake.
// Qualified channels keep carrying application/rekeys after that search ends.
// Meter only their identified service packets, with actual ciphertext + IPv4/UDP
// bytes. An active search ledger is conjunctive with the maintenance window;
// canceling/replacing a child probe never replenishes that profile's allowance.
func (c *assignedCipher) chargeService(size int, required *probeCycle) (result error) {
	defer func() {
		if errors.Is(result, ErrProbeBudget) {
			// An asynchronous send failure must reach the owner as local quota
			// exhaustion, rather than look like a remote path timeout three
			// seconds later and evict a working Direct route.
			c.mu.Lock()
			p := c.probe
			c.mu.Unlock()
			if p != nil {
				p.attempt.abort(ErrProbeBudget)
			}
		}
	}()
	if size <= 0 || size > 65507 {
		return ErrProbeBudget
	}
	c.bind.mu.Lock()
	confirmed := c.bind.receiveConfirmed || c.bind.transmitConfirmed
	c.bind.mu.Unlock()
	if !confirmed {
		return nil
	}
	c.meterMu.Lock()
	defer c.meterMu.Unlock()
	window, err := c.serviceCycleLocked()
	if err != nil {
		return err
	}
	if required != nil && required != window {
		return ErrNoAuthority
	}
	if err := window.charge(size + 28); err != nil {
		return err
	}
	c.mu.Lock()
	p := c.probe
	c.mu.Unlock()
	if p != nil {
		if err := p.attempt.cycle.check(); err == nil {
			return p.attempt.cycle.charge(size + 28)
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	return nil
}

func (c *assignedCipher) serviceReady() error {
	c.meterMu.Lock()
	defer c.meterMu.Unlock()
	if c.serviceWindow == nil {
		return nil
	}
	window, err := c.serviceCycleLocked()
	if err != nil {
		return err
	}
	if window.wire.consumed() >= transport.MaxProbeBytes {
		return ErrProbeBudget
	}
	return nil
}

// Called under the bind's sendGate after its general map deadline is installed.
// Preserve the specific probe/echo receipt's shorter original deadline too.
func (c *assignedCipher) serviceWriteDeadline(attempt *probeAttempt) error {
	if err := attempt.check(); err != nil {
		return err
	}
	remaining, err := attempt.timer.Remaining()
	if err != nil {
		return err
	}
	cycleRemaining, err := attempt.cycle.remaining()
	if err != nil {
		return err
	}
	sample, err := c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	deadline := sample.Wall.Add(min(remaining, cycleRemaining))
	if end, ok := attempt.ctx.Deadline(); ok {
		deadline = minTime(deadline, end)
	}
	c.bind.mu.Lock()
	socket := c.bind.socket
	deadline = minTime(deadline, c.bind.assignment.plan.ExpiresAt)
	if end := c.bind.assignment.lease.Permissions().ExpiresAt; !end.IsZero() {
		deadline = minTime(deadline, end)
	}
	c.bind.mu.Unlock()
	if socket == nil || socket.SetWriteDeadline(deadline) != nil {
		return ErrPacketIO
	}
	return attempt.check()
}

func (c *assignedCipher) controlSendGuard(raw []byte) func(int) error {
	a, p, closed := c.controlSnapshot()
	local, peer, err := a.addresses()
	if err != nil {
		return func(int) error { return ErrNoAuthority }
	}
	value, claimed, err := decodeControlPacket(raw, local, peer)
	if !claimed {
		return nil // Ordinary application packet; no search quota or probe context.
	}
	if err != nil || closed {
		return func(int) error { return ErrNoAuthority }
	}
	if value.Kind == transport.Challenge && p != nil && sameControl(value, p.request) {
		return func(size int) error {
			if err := p.admit(); err != nil {
				return err
			}
			if err := c.chargeService(size, nil); err != nil {
				return err
			}
			return c.serviceWriteDeadline(p.attempt)
		}
	}
	if value.Kind == transport.Echo {
		c.meterMu.Lock()
		receipt := c.serviceReceipts[controlKey(value)]
		c.meterMu.Unlock()
		return func(size int) error {
			if err := c.receiptCurrent(receipt); err != nil {
				return err
			}
			if err := c.chargeService(size, receipt.window); err != nil {
				return err
			}
			return c.serviceWriteDeadline(receipt.attempt)
		}
	}
	return func(int) error { return ErrNoAuthority }
}

// The supplier calls this only after AEAD/replay/AllowedIPs and IP trimming.
// Bad/reserved controls are dropped here, never delivered into the OS. A peer
// challenge may permit an Echo but cannot satisfy our own path evidence.
func (c *assignedCipher) controlReceived(raw []byte, wireBytes int) error {
	a, p, closed := c.controlSnapshot()
	local, peer, err := a.addresses()
	if err != nil {
		return err
	}
	value, claimed, err := decodeControlPacket(raw, peer, local)
	if !claimed {
		return nil
	}
	if err != nil || closed {
		return ErrNoAuthority
	}
	if err := c.bind.controlBindingAllowed(value, true); err != nil {
		return err
	}
	if value.Kind == transport.Echo {
		if p == nil || !transport.MatchesReply(p.request, value) {
			return ErrNoAuthority
		}
		if err := p.admit(); err != nil {
			return err
		}
		return c.chargeService(wireBytes, nil)
	}
	if value.Kind != transport.Challenge {
		return ErrNoAuthority
	}
	c.bind.mu.Lock()
	confirmed := c.bind.receiveConfirmed || c.bind.transmitConfirmed
	c.bind.mu.Unlock()
	if !confirmed {
		if p == nil {
			return ErrNoAuthority
		}
		if err := p.admit(); err != nil {
			return err
		}
		// During discovery each endpoint has one frozen candidate ID.
		if value.CandidateID != p.request.CandidateID {
			return ErrNoAuthority
		}
	}
	receipt, err := c.rememberChallenge(a, value)
	if err != nil {
		return err
	}
	return c.chargeService(wireBytes, receipt.window)
}
