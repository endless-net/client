//go:build windows

package dataplane

import (
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
)

// ConnectionLocal never waits for an in-flight native SDK transaction and never
// treats a journal or listener as a confirmed path. A busy owner is unknown.
func (s *WindowsSession) ConnectionLocal() connectivity.LocalObservation {
	if !s.mu.TryLock() {
		return connectivity.LocalObservation{Permission: s.lease.Permissions()}
	}
	defer s.mu.Unlock()
	if !s.started || s.stopped || s.engine == nil || s.packets.Err() != nil {
		return connectivity.LocalObservation{Permission: s.lease.Permissions()}
	}
	m, err := s.current.Snapshot()
	if err != nil {
		return connectivity.LocalObservation{}
	}
	paths, _, closed := s.engine.direct.snapshot()
	if closed {
		return connectivity.LocalObservation{}
	}
	v := connectivity.LocalObservation{Known: true, Applied: true, MapRevision: m.Revision.Network, Address: m.Node.AssignedIP, PeerCount: uint32(len(m.Peers)), GeneratedAt: m.MapSignature.IssuedAt}
	choices := s.packets.outputChoices()
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
	v.Permission = s.lease.Permissions()
	v.SoleNodeReady = v.PeerCount == 0 && s.tunnelReady()
	if s.supervisor != nil {
		v.PeerTransports = s.supervisor.transportStatuses(s.current)
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
