package dataplane

import (
	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// TransportCapabilities describes only the compiled, pinned engines and closed
// profile interpreter. It is discovery metadata, never path/access authority.
func TransportCapabilities() transport.Capabilities {
	engines := make([]transport.Capability, len(compiledPeerEngines))
	for i, engine := range compiledPeerEngines {
		engines[i] = engine.capability
	}
	return transport.Capabilities{Version: transport.Version, Engines: engines}
}

func transportAdvertisement(generation uint64) coordinator.TransportAdvertisement {
	return coordinator.TransportAdvertisement{IntentGeneration: generation, Capabilities: TransportCapabilities(), Endpoints: []coordinator.TransportEndpoint{}}
}
