//go:build windows

package dataplane

import (
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
	return transportObservation(s.current, s.lease, s.engine, s.packets, s.supervisor, s.tunnelReady())
}
