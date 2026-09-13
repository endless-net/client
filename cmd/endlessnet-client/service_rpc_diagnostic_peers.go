package main

import (
	"net/netip"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Join live public keys only against the verified profile map. No endpoint or
// handshake is enough to infer selected path, reachability, or a peer identity.
func nativeDiagnosticPeers(networkMap clientapi.RegisterNodeResponse, inspection client.WireGuardInspection) ([]*ipc.Peer, []*ipc.TunnelPeer, []*ipc.Failure) {
	fail := func(key string) ([]*ipc.Peer, []*ipc.TunnelPeer, []*ipc.Failure) {
		return nil, nil, []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: key}}
	}
	if len(networkMap.Peers) > 4096 || len(inspection.Peers) > 4096 {
		return fail("diagnostics_peer_limit_exceeded")
	}
	byKey := map[string]string{}
	ids := map[string]bool{}
	var peers []*ipc.Peer
	var overlay []netip.Prefix
	for _, raw := range []string{networkMap.Network.CIDR, networkMap.Network.IPv6CIDR} {
		if prefix, err := netip.ParsePrefix(raw); err == nil {
			overlay = append(overlay, prefix)
		}
	}
	for _, peer := range networkMap.Peers {
		if peer.ID == "" || peer.PublicKey == "" || ids[peer.ID] || byKey[peer.PublicKey] != "" || (peer.NetworkID != "" && peer.NetworkID != networkMap.Network.ID) {
			return fail("diagnostics_peer_map_invalid")
		}
		ids[peer.ID] = true
		byKey[peer.PublicKey] = peer.ID
		native := &ipc.Peer{Id: peer.ID, Hostname: peer.Hostname}
		seen := map[string]bool{}
		for _, raw := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil || prefix.Bits() != prefix.Addr().BitLen() {
				continue
			}
			for _, network := range overlay {
				address := prefix.Addr().String()
				if network.Contains(prefix.Addr()) && !seen[address] {
					native.OverlayAddresses = append(native.OverlayAddresses, address)
					seen[address] = true
				}
			}
		}
		peers = append(peers, native)
	}
	var live []*ipc.TunnelPeer
	var failures []*ipc.Failure
	if !inspection.OK {
		return peers, nil, nil
	}
	seen := map[string]bool{}
	mismatch := false
	for _, peer := range inspection.Peers {
		id := byKey[peer.PublicKey]
		if id == "" || seen[id] || peer.LatestHandshakeUnix < 0 || peer.LatestHandshakeUnix > 253402300799 || peer.PersistentKeepaliveSeconds < 0 || peer.PersistentKeepaliveSeconds > 65535 {
			mismatch = true
			continue
		}
		seen[id] = true
		entry := &ipc.TunnelPeer{PeerId: id, PublicKey: peer.PublicKey, Endpoint: peer.Endpoint,
			AllowedIps: append([]string(nil), peer.AllowedIPs...), ReceivedBytes: peer.TransferRXBytes, TransmittedBytes: peer.TransferTXBytes,
			PersistentKeepalive: durationpb.New(time.Duration(peer.PersistentKeepaliveSeconds) * time.Second)}
		if peer.LatestHandshakeUnix > 0 {
			entry.LatestHandshake = timestamppb.New(time.Unix(peer.LatestHandshakeUnix, 0))
		}
		live = append(live, entry)
	}
	if mismatch {
		failures = append(failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "diagnostics_tunnel_peer_map_mismatch"})
	}
	return peers, live, failures
}
