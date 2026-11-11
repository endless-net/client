package dataplane

import (
	"encoding/json"
	"net/netip"
	"sort"

	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type preparedListener struct {
	id, digest string
	port       uint16
}

func (g *transportSupervisor) preparedListeners() []preparedListener {
	seen := make(map[*assignedCipher]bool)
	byID := make(map[string]preparedListener)
	for _, p := range g.peers {
		channels := []*assignedCipher{p.direct, p.spare}
		if p.search != nil {
			channels = append(channels, p.search.candidate)
		}
		for _, c := range channels {
			if c == nil || seen[c] {
				continue
			}
			seen[c] = true
			port, err := c.cipher.BoundPort()
			if err != nil {
				continue
			}
			c.bind.mu.Lock()
			a := c.bind.assignment
			c.bind.mu.Unlock()
			byID[a.offer.Profile.ID] = preparedListener{a.offer.Profile.ID, a.offer.Digest, port}
		}
	}
	result := make([]preparedListener, 0, len(byID))
	for _, ep := range byID {
		result = append(result, ep)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func listenerAdvertisement(generation uint64, listeners []preparedListener, address netip.Addr) coordinator.TransportAdvertisement {
	ad := transportAdvertisement(generation)
	for _, ep := range listeners {
		ad.Endpoints = append(ad.Endpoints, coordinator.TransportEndpoint{ProfileID: ep.id, ProfileDigest: ep.digest, Endpoint: netip.AddrPortFrom(address, ep.port).String()})
	}
	return ad
}

func (g *transportSupervisor) listenersFit() bool {
	// Include the whole existing Gateway/Coordinator request envelope, with
	// maximum numeric generations and IPv4 endpoint width, not only the ad.
	ad := listenerAdvertisement(g.generation, g.preparedListeners(), netip.MustParseAddr("255.255.255.254"))
	if coordinator.ValidateTransportAdvertisement(ad) != nil {
		return false
	}
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "255.255.255.254:65535", Generation: ^uint64(0), TTL: "5m0s", Status: "online", Transport: &ad}
	raw, err := json.Marshal(request)
	return err == nil && len(raw) <= transport.MaxProfileBytes
}

func (g *transportSupervisor) publishListeners() {
	listeners := g.preparedListeners()
	g.mu.Lock()
	changed := len(listeners) != len(g.listeners)
	if !changed {
		for i, ep := range listeners {
			if ep != g.listeners[i] {
				changed = true
				break
			}
		}
	}
	g.listeners = listeners
	g.mu.Unlock()
	if changed && g.config.advertise != nil {
		g.config.advertise()
	}
}

func (g *transportSupervisor) advertisement(address netip.Addr) coordinator.TransportAdvertisement {
	g.mu.Lock()
	listeners := append([]preparedListener(nil), g.listeners...)
	g.mu.Unlock()
	return listenerAdvertisement(g.generation, listeners, address)
}
