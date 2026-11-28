//go:build windows

package dataplane

import (
	"github.com/endless-net/client/internal/connectivity"
	"google.golang.org/protobuf/proto"
)

func (s *WindowsSession) publishAppliedView() {
	s.observed.Store(&appliedView{s.current, s.engine, s.packets, s.supervisor})
}

// ConnectionLocal never waits for an in-flight native SDK transaction and never
// treats a journal or listener as a confirmed path.
func (s *WindowsSession) ConnectionLocal() connectivity.LocalObservation {
	if !s.mu.TryLock() {
		if view := s.observed.Load(); view != nil {
			local := transportObservation(view.value, s.lease, view.engine, view.packets, view.supervisor, false)
			local.DNS = proto.CloneOf(s.dnsResult.Load())
			return local
		}
		return connectivity.LocalObservation{Permission: s.lease.Permissions(), DNS: proto.CloneOf(s.dnsResult.Load())}
	}
	defer s.mu.Unlock()
	if !s.started || s.stopped || s.engine == nil || s.packets.Err() != nil {
		return connectivity.LocalObservation{Permission: s.lease.Permissions(), DNS: proto.CloneOf(s.dnsResult.Load())}
	}
	local := transportObservation(s.current, s.lease, s.engine, s.packets, s.supervisor, s.tunnelReady())
	local.DNS = proto.CloneOf(s.dnsResult.Load())
	return local
}
