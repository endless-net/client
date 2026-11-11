package dataplane

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

// confirmedAdmission admits ciphertext/rekey responses for a qualified channel.
// Application output additionally requires transmitConfirmed and its captured
// epoch. Current map/profile/endpoint/lease always bound both directions.
func (b *candidateBind) confirmedAdmission(endpoint netip.AddrPort, epoch uint64) (candidateSocket, time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil || b.socket == nil {
		return nil, time.Time{}, net.ErrClosed
	}
	if !b.receiveConfirmed && !b.transmitConfirmed || b.confirmationEpoch != epoch || !b.endpointsReady() || endpoint.String() != b.assignment.offer.PeerEndpoint {
		return nil, time.Time{}, ErrNoAuthority
	}
	sample, err := b.clock.Sample()
	if err != nil {
		return nil, time.Time{}, connectivity.ErrClock
	}
	if err := b.assignment.permission(sample.Wall, false); err != nil {
		return nil, time.Time{}, err
	}
	deadline := minTime(sample.Wall.Add(3*time.Second), b.assignment.plan.ExpiresAt)
	if expires := b.assignment.lease.Permissions().ExpiresAt; expires.Before(deadline) {
		deadline = expires
	}
	return b.socket, deadline, nil
}

func (b *candidateBind) sendConfirmed(raw []byte, endpoint netip.AddrPort, epoch uint64, guard func(int) error) error {
	ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.sendGate:
	}
	defer func() { b.sendGate <- struct{}{} }()
	socket, deadline, err := b.confirmedAdmission(endpoint, epoch)
	if err != nil {
		return err
	}
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if socket.SetWriteDeadline(deadline) != nil {
		return ErrPacketIO
	}
	if _, _, err := b.confirmedAdmission(endpoint, epoch); err != nil {
		return err
	}
	if guard != nil {
		if err := guard(len(raw)); err != nil {
			return err
		}
	}
	if _, _, err := b.confirmedAdmission(endpoint, epoch); err != nil {
		return err
	}
	count, err := socket.WriteToUDPAddrPort(raw, endpoint)
	if err != nil || count != len(raw) {
		return ErrPacketIO
	}
	_, _, err = b.confirmedAdmission(endpoint, epoch)
	return err
}

func (b *candidateBind) transmitEpoch() (uint64, error) {
	b.mu.Lock()
	allowed, epoch, endpoint := b.transmitConfirmed, b.confirmationEpoch, b.assignment.offer.PeerEndpoint
	b.mu.Unlock()
	if !allowed {
		return 0, ErrNoAuthority
	}
	address, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return 0, ErrNoAuthority
	}
	if _, _, err := b.confirmedAdmission(address, epoch); err != nil {
		return 0, err
	}
	return epoch, nil
}
func (b *candidateBind) transmitCurrent(epoch uint64) bool {
	current, err := b.transmitEpoch()
	return err == nil && current == epoch
}

func (c *assignedCipher) Activate(value *directActivation) error {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()
	if value == nil || value.direct != c {
		return ErrNoAuthority
	}
	if err := value.check(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.bind.mu.Lock()
	defer c.bind.mu.Unlock()
	sample, err := c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if err := c.bind.assignment.permission(sample.Wall, true); err != nil {
		return err
	}
	if c.bind.closed || c.bind.ctx.Err() != nil || c.bind.probe != value.qualified.latest.attempt || !c.bind.endpointsReady() {
		return ErrNoAuthority
	}
	if err := value.qualified.latest.attempt.check(); err != nil {
		return err
	}
	if !c.bind.assignment.sameProbe(value.qualified.stability.assignment) {
		return ErrNoAuthority
	}
	if !value.used.CompareAndSwap(false, true) {
		return ErrNoAuthority
	}
	c.bind.receiveConfirmed = true
	c.bind.transmitConfirmed = true
	clear(c.bind.lastHandshake)
	c.bind.lastHandshake, c.bind.handshakeAttempt = nil, nil
	c.bind.confirmationEpoch++
	return nil
}

// AllowReceive retains a locally qualified channel while the initiator waits
// for a commit ACK. It grants no application output. The peer owner accounts
// this receiver in its existing Direct/transient engine slots; it is not an
// unbounded collection of accepted old candidates.
func (c *assignedCipher) AllowReceive(value *directReceiveGrant) error {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()
	if value == nil || value.direct != c {
		return ErrNoAuthority
	}
	if err := value.check(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	c.bind.mu.Lock()
	defer c.bind.mu.Unlock()
	sample, err := c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if err := c.bind.assignment.permission(sample.Wall, true); err != nil {
		return err
	}
	if c.bind.closed || c.bind.ctx.Err() != nil || c.bind.probe != value.qualified.latest.attempt || !c.bind.endpointsReady() {
		return ErrNoAuthority
	}
	if err := value.qualified.latest.attempt.check(); err != nil {
		return err
	}
	if !c.bind.assignment.sameProbe(value.qualified.stability.assignment) {
		return ErrNoAuthority
	}
	if !value.used.CompareAndSwap(false, true) {
		return ErrNoAuthority
	}
	if !c.bind.receiveConfirmed {
		c.bind.receiveConfirmed = true
		clear(c.bind.lastHandshake)
		c.bind.lastHandshake, c.bind.handshakeAttempt = nil, nil
		c.bind.confirmationEpoch++
	}
	return nil
}

func (c *assignedCipher) QueueApplication(raw []byte) error { return c.queueApplication(raw, nil) }

func (c *assignedCipher) queueApplication(raw []byte, routeGuard func() bool) error {
	epoch, err := c.bind.transmitEpoch()
	if err != nil {
		return err
	}
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	source, destination, err := a.addresses()
	if err != nil {
		return err
	}
	if err := applicationForPair(raw, source, destination); err != nil {
		return err
	}
	guard := func() bool { return c.bind.transmitCurrent(epoch) && (routeGuard == nil || routeGuard()) }
	if err := c.cipher.queueChecked(raw, false, guard); err != nil {
		return err
	}
	if !guard() {
		return ErrNoAuthority
	}
	return nil
}

func (b *candidateBind) controlAllowed(value transport.Control, incoming bool) error {
	b.mu.Lock()
	known := b.receiveConfirmed || b.transmitConfirmed
	b.mu.Unlock()
	if !known {
		return ErrNoAuthority
	}
	return b.controlBindingAllowed(value, incoming)
}

func (b *candidateBind) controlBindingAllowed(value transport.Control, incoming bool) error {
	if transport.ValidateControl(value) != nil {
		return ErrNoAuthority
	}
	b.mu.Lock()
	a := b.assignment
	b.mu.Unlock()
	sample, err := b.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if err := a.permission(sample.Wall, true); err != nil {
		return err
	}
	localID, peerID := a.plan.LocalNodeID, a.plan.PeerNodeID
	localKey, peerKey := a.plan.LocalPublicKey, a.plan.PeerPublicKey
	localGen, peerGen := a.plan.LocalIntentGeneration, a.plan.PeerAdvertisement.IntentGeneration
	if incoming {
		localID, peerID = peerID, localID
		localKey, peerKey = peerKey, localKey
		localGen, peerGen = peerGen, localGen
	}
	if value.AccountID != a.plan.AccountID || value.NetworkID != a.plan.NetworkID || value.SourceNodeID != localID || value.DestinationNodeID != peerID || value.SourcePublicKey != localKey || value.DestinationPublicKey != peerKey || value.SourceGeneration != localGen || value.DestinationGeneration != peerGen || !b.revisionAllowed(value.NetworkRevision) || value.ProfileDigest != a.offer.Digest {
		return ErrNoAuthority
	}
	return nil
}

// Echoing a fresh current peer challenge on an already qualified receive path
// sustains peer liveness after the search attempt. It creates no local proof.
func (c *assignedCipher) echoConfirmed(value transport.Control) error {
	if value.Kind != transport.Challenge {
		return ErrNoAuthority
	}
	if err := c.bind.controlAllowed(value, true); err != nil {
		return err
	}
	reply := replyControl(value, transport.Echo)
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	source, destination, err := a.addresses()
	if err != nil {
		return err
	}
	raw, err := controlPacket(source, destination, reply)
	if err != nil {
		return err
	}
	if err := c.cipher.Queue(raw, true); err != nil {
		return err
	}
	return c.bind.controlAllowed(reply, false)
}
