package dataplane

import (
	"net/netip"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
)

func transportObservation(value connectivity.Map, lease *connectivity.Lease, engine *Engine, packets *packetSwitch, supervisor *transportSupervisor, ready bool) connectivity.LocalObservation {
	if engine == nil || packets == nil || packets.Err() != nil {
		return connectivity.LocalObservation{Permission: lease.Permissions()}
	}
	m, err := value.Snapshot()
	if err != nil {
		return connectivity.LocalObservation{}
	}
	paths, _, closed := engine.direct.snapshot()
	if closed {
		return connectivity.LocalObservation{}
	}
	v := connectivity.LocalObservation{Known: true, Applied: true, MapRevision: m.Revision.Network, MapPayloadHash: value.Cursor().PayloadHash, Address: m.Node.AssignedIP, PeerCount: uint32(len(m.Peers)), GeneratedAt: m.MapSignature.IssuedAt}
	choices := packets.outputChoices()
	for _, path := range paths {
		if _, overridden := choices[path.PeerID]; path.Authenticated && !overridden {
			v.DirectPaths++
		}
	}
	for _, choice := range choices {
		switch choice {
		case peerRouteRelay:
			v.RelayPaths++
		case peerRouteDirect:
			v.DirectPaths++
		}
	}
	v.Permission = lease.Permissions()
	if v.Permission.NewFlows && lease.MatchesMap(value) {
		v.Resources = packets.observedResources(value, m)
		v.Peers = make([]*clientv0.Peer, 0, len(m.Peers))
		for _, peer := range m.Peers {
			address, err := netip.ParsePrefix(peer.AllowedIPs[0])
			if err == nil {
				v.Peers = append(v.Peers, &clientv0.Peer{
					Id: peer.ID, Hostname: peer.Hostname,
					OverlayAddresses: []string{address.Addr().String()},
				})
			}
		}
	}
	v.SoleNodeReady = v.PeerCount == 0 && ready
	if supervisor != nil {
		v.PeerTransports = supervisor.transportStatuses(value)
	}
	if len(v.PeerTransports) == 0 && len(m.Peers) > 0 {
		// A newly applied map may precede the supervisor's first observation.
		v.PeerTransports = make([]*clientv0.PeerTransportStatus, 0, len(m.Peers))
		for _, peer := range m.Peers {
			v.PeerTransports = append(v.PeerTransports, pendingPeerTransport(peer.ID))
		}
	}
	return v
}
