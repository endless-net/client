package dataplane

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	relay "github.com/endless-net/relay/api/wire"
	wgdevice "github.com/tailscale/wireguard-go/device"
)

const relayMTUMax = (relay.MaxFramePayloadBytes - wgdevice.MessageTransportSize) &^ 15

// relayCipher reserves a baseline end-to-end WireGuard device for one peer.
// Downloaded obfuscation profiles do not mutate this working fallback. Its
// logical bind port is never exposed as a Direct listener or reachability proof.
type relayCipher struct {
	mu          sync.Mutex
	cipher      *peerCipher
	bind        *relayPeerBind
	cancel      context.CancelFunc
	events      chan transport.Control
	done        chan struct{}
	sink        func([]byte) error
	closed      bool
	health      *relayHealthProbe
	reservation *engineReservation
	hadSession  atomic.Bool
	sessionLost atomic.Bool
}

func (*relayCipher) String() string   { return "[encrypted Relay peer channel]" }
func (*relayCipher) GoString() string { return "[encrypted Relay peer channel]" }

func relayBaseline() transport.Profile {
	return transport.Profile{Version: 1, ID: "relay-wireguard", Revision: 1, Engine: transport.WireGuard, ProtocolVersion: "1", Prelude: []transport.Datagram{}}
}

func newRelayCipher(parent context.Context, mux *relayMux, value connectivity.Map, lease *connectivity.Lease, peerID string, private []byte, mtu int, sink func([]byte) error) (*relayCipher, error) {
	if parent == nil || parent.Err() != nil || mux == nil || sink == nil || mtu < 576 || mtu > relayMTUMax {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	bind, err := mux.newBind(ctx, value, lease, peerID)
	if err != nil {
		cancel()
		return nil, err
	}
	c := &relayCipher{bind: bind, cancel: cancel, events: make(chan transport.Control, portQueueFrames), done: make(chan struct{}), sink: sink}
	port, err := newPacketPort(ctx, mtu, bind.permissions, c.receivePlaintext)
	if err != nil {
		bind.shutdown()
		cancel()
		return nil, err
	}
	bind.mu.Lock()
	local, peer := bind.local, bind.peer
	peer.Endpoint = bind.endpoint.String()
	bind.mu.Unlock()
	_, err = buildPeerCipher(ctx, local, peer, private, relayBaseline(), port, bind, func(_ wgdevice.NoisePublicKey, state wgdevice.PeerSessionState) {
		if state == wgdevice.PeerSessionEstablished {
			c.hadSession.Store(true)
			c.sessionLost.Store(false)
		} else if c.hadSession.Load() {
			c.sessionLost.Store(true)
		}
	}, func(cipher *peerCipher) { c.cipher = cipher })
	if err != nil {
		_ = port.Close()
		bind.shutdown()
		cancel()
		return nil, err
	}
	// A missed control reply must not destroy an existing app channel merely
	// because the other Client temporarily cannot start new flows. The pinned
	// WG lifecycle remains the cryptographic session-loss boundary.
	p := bind.lease.Permissions()
	if p.Revoked || p.ClockUnsafe || p.ExpiresAt.IsZero() || !mux.transport.config.Now().Before(p.ExpiresAt) {
		c.Close()
		return nil, ErrNoAuthority
	}
	context.AfterFunc(bind.ctx, c.Close)
	return c, nil
}

func (b *relayPeerBind) controlAllowed(value transport.Control, incoming bool) error {
	if transport.ValidateControl(value) != nil || !b.permissions().NewFlows {
		return ErrNoAuthority
	}
	b.mu.Lock()
	local, peer, m, account := b.local, b.peer, b.mapValue, b.account
	floor := b.revisionFloor
	b.mu.Unlock()
	if local.Transport == nil || peer.Transport == nil || local.Transport.IntentGeneration != m.IntentGeneration() || peer.Transport.LocalIntentGeneration != m.IntentGeneration() {
		return ErrNoAuthority
	}
	localID, remoteID := local.ID, peer.ID
	localKey, remoteKey := local.PublicKey, peer.PublicKey
	localGeneration, peerGeneration := m.IntentGeneration(), peer.Transport.PeerAdvertisement.IntentGeneration
	if incoming {
		localID, remoteID = remoteID, localID
		localKey, remoteKey = remoteKey, localKey
		localGeneration, peerGeneration = peerGeneration, localGeneration
	}
	if value.AccountID != account || value.NetworkID != b.mux.transport.network || value.NetworkRevision < floor || value.NetworkRevision > m.Cursor().Revision.Network || value.SourceNodeID != localID || value.DestinationNodeID != remoteID || value.SourcePublicKey != localKey || value.DestinationPublicKey != remoteKey || value.SourceGeneration != localGeneration || value.DestinationGeneration != peerGeneration {
		return ErrNoAuthority
	}
	baseline, err := transport.ProfileDigest(relayBaseline())
	if err != nil {
		return err
	}
	if value.ProfileDigest == baseline {
		if value.Kind != transport.Challenge && value.Kind != transport.Echo {
			return ErrNoAuthority
		}
		return nil
	}
	now := b.mux.transport.config.Now()
	if now.Before(peer.Transport.IssuedAt) || !now.Before(peer.Transport.ExpiresAt) {
		return ErrNoAuthority
	}
	for _, offer := range peer.Transport.Profiles {
		if offer.Digest == value.ProfileDigest {
			return nil
		}
	}
	return ErrNoAuthority
}

func (c *relayCipher) QueueControl(value transport.Control) error {
	if err := c.bind.controlAllowed(value, false); err != nil {
		return err
	}
	c.bind.mu.Lock()
	a := candidateAssignment{local: c.bind.local, peer: c.bind.peer}
	c.bind.mu.Unlock()
	source, destination, err := a.addresses()
	if err != nil {
		return err
	}
	packet, err := controlPacket(source, destination, value)
	if err != nil {
		return err
	}
	if err := c.cipher.Queue(packet, true); err != nil {
		return err
	}
	return c.bind.controlAllowed(value, false)
}

func (c *relayCipher) QueueApplication(raw []byte) error {
	return c.queueApplication(raw, nil)
}
func (c *relayCipher) queueApplication(raw []byte, routeGuard func() bool) error {
	c.bind.mu.Lock()
	a := candidateAssignment{local: c.bind.local, peer: c.bind.peer}
	c.bind.mu.Unlock()
	source, destination, err := a.addresses()
	if err != nil {
		return err
	}
	if err := applicationForPair(raw, source, destination); err != nil {
		return err
	}
	if err := c.cipher.queueChecked(raw, false, routeGuard); err != nil {
		return err
	}
	if routeGuard != nil && !routeGuard() {
		return ErrNoAuthority
	}
	return nil
}

func (c *relayCipher) receivePlaintext(raw []byte) error {
	c.bind.mu.Lock()
	a := candidateAssignment{local: c.bind.local, peer: c.bind.peer}
	c.bind.mu.Unlock()
	local, peer, err := a.addresses()
	if err != nil {
		return err
	}
	value, claimed, err := decodeControlPacket(raw, peer, local)
	if claimed {
		if err != nil || c.bind.controlAllowed(value, true) != nil {
			return nil
		}
		if c.receiveHealth(value) {
			return nil
		}
		// The engine writer never blocks on the supervisor. The strict codec bounds
		// every event to <=1200 bytes, so sixteen events also fit the 64KiB queue cap.
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed {
			return nil
		}
		select {
		case c.events <- value:
		default:
		}
		return nil
	}
	if err != nil {
		return err
	}
	return c.sink(raw)
}

func (c *relayCipher) Events() <-chan transport.Control { return c.events }
func (c *relayCipher) Close() {
	c.mu.Lock()
	if c.closed {
		done := c.done
		c.mu.Unlock()
		<-done
		return
	}
	c.closed = true
	health := c.health
	reservation := c.reservation
	c.mu.Unlock()
	if health != nil {
		health.attempt.cancel()
	}
	c.cancel()
	c.bind.shutdown()
	c.cipher.Close()
	if reservation != nil {
		reservation.release()
	}
	close(c.events)
	close(c.done)
}
