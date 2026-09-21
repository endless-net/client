package client

import (
	"net/netip"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/device"
)

// A reachable relay connection alone proves nothing about the selected peer.
// The authenticated peer handshake must have completed on this bridge and after
// this path selection. Native routes and all packet-filter checks remain the
// collector's separate prerequisites; this is not application health evidence.
func resourceHostRelayPathObserved(paths []PeerPathStatus, peer api.Peer, live WireGuardPeerInspection, bridge wireGuardRelayPeerObservation, now time.Time) bool {
	handshake, complete := live.authenticatedHandshakeTime()
	if !complete || handshake.After(now) || !now.Before(handshake.Add(device.RejectAfterTime)) || bridge.ReadyAt.IsZero() || !handshake.After(bridge.ReadyAt) || bridge.PeerID != peer.ID || bridge.PublicKey != peer.PublicKey || bridge.Endpoint != live.Endpoint {
		return false
	}
	local, err := netip.ParseAddrPort(bridge.Endpoint)
	if err != nil || !local.Addr().IsLoopback() || local.Port() == 0 {
		return false
	}
	found := false
	for _, path := range paths {
		if path.PeerID != peer.ID {
			continue
		}
		if found || path.SelectedPath != "relay" || path.SelectedEndpoint != bridge.RelayAddress || path.Relay.State != "reachable" || path.Relay.Endpoint != bridge.RelayAddress || path.Relay.RelayID != bridge.RelayID || path.Relay.Protocol != bridge.RelayProtocol {
			return false
		}
		transition, err := time.Parse(time.RFC3339Nano, path.LastTransitionAt)
		if err != nil || !handshake.After(transition) {
			return false
		}
		found = true
	}
	return found
}
