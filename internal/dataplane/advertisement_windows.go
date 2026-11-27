//go:build windows

package dataplane

import (
	"maps"
	"net/netip"
	"sort"
	"strings"

	"github.com/endless-net/client/api/transport"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

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
		interfaces, err := LANIPv4Interfaces()
		if err != nil {
			return coordinator.TransportAdvertisement{}, err
		}
		if len(interfaces) > s.config.MaxInventoryRows {
			return coordinator.TransportAdvertisement{}, ErrTransportCapacity
		}
		addresses := make([]netip.Addr, 0, len(interfaces))
		for address := range interfaces {
			addresses = append(addresses, address)
		}
		sort.Slice(addresses, func(i, j int) bool { return addresses[i].Less(addresses[j]) })
		s.lanInterfaces = interfaces
		s.supervisor.mu.Lock()
		listeners := append([]preparedListener(nil), s.supervisor.listeners...)
		s.supervisor.mu.Unlock()
		return lanAdvertisement(s.generation, listeners, addresses)
	}
	return transportAdvertisement(s.generation), nil
}

func (s *WindowsSession) AdvertisementSignal() <-chan struct{} { return s.adsChanged }

// LANIPv4Interfaces returns eligible source addresses on active interfaces, including connected-only LANs.
func LANIPv4Interfaces() (map[netip.Addr]uint32, error) {
	interfaces, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormalWithoutStatistics)
	if err != nil {
		return nil, err
	}
	active := make(map[uint32]bool)
	for i := range interfaces {
		row := &interfaces[i]
		if row.OperStatus == winipcfg.IfOperStatusUp && row.Type != winipcfg.IfTypeSoftwareLoopback && !strings.HasPrefix(strings.ToLower(row.Alias()), "endlessnet-") {
			active[row.InterfaceIndex] = true
		}
	}
	rows, err := winipcfg.GetUnicastIPAddressTable(windows.AF_INET)
	if err != nil {
		return nil, err
	}
	addresses := make(map[netip.Addr]uint32)
	for _, row := range rows {
		ip := row.Address.Addr()
		if active[row.InterfaceIndex] && row.DadState == winipcfg.DadStatePreferred && !row.SkipAsSource && ip.Is4() && ip.IsGlobalUnicast() {
			if prior, ok := addresses[ip]; !ok || row.InterfaceIndex < prior {
				addresses[ip] = row.InterfaceIndex
			}
		}
	}
	return addresses, nil
}

// Check secondary LANs on the existing probe tick. A changed inventory only
// requests a new announcement; it does not restart the session or drop Relay.
func (s *WindowsSession) checkLANChanges() {
	interfaces, err := LANIPv4Interfaces()
	if err == nil && !maps.Equal(interfaces, s.lanInterfaces) {
		s.lanInterfaces = interfaces
		select {
		case s.adsChanged <- struct{}{}:
		default:
		}
	}
}

func lanAdvertisement(generation uint64, listeners []preparedListener, addresses []netip.Addr) (coordinator.TransportAdvertisement, error) {
	// Reject any product whose string contents alone exceed the envelope,
	// before allocating the full advertisement.
	size := 0
	for _, listener := range listeners {
		size += len(addresses) * (len(listener.id) + len(listener.digest))
		if size > transport.MaxProfileBytes {
			return coordinator.TransportAdvertisement{}, ErrTransportCapacity
		}
	}
	ad := listenerAdvertisement(generation, listeners, addresses...)
	if !announcementFits(ad) {
		return coordinator.TransportAdvertisement{}, ErrTransportCapacity
	}
	return ad, nil
}
