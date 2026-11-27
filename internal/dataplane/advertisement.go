package dataplane

import (
	"bytes"
	"crypto/rand"
	"sync"

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

// Keep the header key stable across endpoint renewals during this process.
var proposalHeaderKey = sync.OnceValue(func() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
})

func transportAdvertisement(generation uint64) coordinator.TransportAdvertisement {
	caps := TransportCapabilities()
	ad := coordinator.TransportAdvertisement{IntentGeneration: generation, Capabilities: caps, Endpoints: []coordinator.TransportEndpoint{}}
	for _, engine := range caps.Engines {
		p := transport.Profile{Version: engine.ProfileVersion, ID: engine.Engine, Revision: 1, Engine: engine.Engine, ProtocolVersion: engine.ProtocolVersion, Prelude: []transport.Datagram{}}
		switch engine.Engine {
		case transport.WireGuard:
		case transport.AmneziaWG:
			p.AWG = &transport.AWG{Headers: []transport.Range{{Min: 10, Max: 19}, {Min: 20, Max: 29}, {Min: 30, Max: 39}, {Min: 40, Max: 49}}, Padding: []uint16{12, 12, 12, 12}, HeaderProtectionKey: bytes.Clone(proposalHeaderKey()), ContentPadding: transport.Range{Max: 32}, RandomTrailers: true}
		default:
			continue
		}
		ad.ProposedProfiles = append(ad.ProposedProfiles, p)
	}
	return ad
}
