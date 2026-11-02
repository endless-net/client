//go:build windows

package dataplane

import "github.com/endless-net/client/internal/connectivity"

// ConnectionLocal never waits for an in-flight native SDK transaction and never
// treats a journal or listener as a confirmed path. A busy owner is unknown.
func (s *WindowsSession) ConnectionLocal() connectivity.LocalObservation {
	if !s.mu.TryLock() {
		return connectivity.LocalObservation{Permission: s.lease.Permissions()}
	}
	defer s.mu.Unlock()
	if !s.started || s.stopped || s.engine == nil {
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
	for _, path := range paths {
		if path.Authenticated {
			v.DirectPaths++
		}
	}
	v.Permission = s.lease.Permissions()
	return v
}
