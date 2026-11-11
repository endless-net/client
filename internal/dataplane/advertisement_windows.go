//go:build windows

package dataplane

import (
	"net/netip"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func (s *WindowsSession) TransportAdvertisement(underlay netip.Addr) (coordinator.TransportAdvertisement, error) {
	if !underlay.Is4() || !underlay.IsGlobalUnicast() || underlay.IsLoopback() {
		return coordinator.TransportAdvertisement{}, ErrConfiguration
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped || s.engine == nil {
		return coordinator.TransportAdvertisement{}, ErrClosed
	}
	if s.supervisor != nil {
		return s.supervisor.advertisement(underlay), nil
	}
	return transportAdvertisement(s.generation), nil
}

func (s *WindowsSession) AdvertisementSignal() <-chan struct{} { return s.adsChanged }
