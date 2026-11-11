package dataplane

import (
	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// TransportCapabilities describes only the compiled, pinned engines and closed
// profile interpreter. It is discovery metadata, never path/access authority.
func TransportCapabilities() transport.Capabilities {
	return transport.Capabilities{Version: transport.Version, Engines: []transport.Capability{
		{Engine: transport.AmneziaWG, ProtocolVersion: "3.1", ProfileVersion: transport.Version},
		{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: transport.Version},
	}}
}

func transportAdvertisement(generation uint64) coordinator.TransportAdvertisement {
	return coordinator.TransportAdvertisement{IntentGeneration: generation, Capabilities: TransportCapabilities(), Endpoints: []coordinator.TransportEndpoint{}}
}
