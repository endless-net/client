package control

import (
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func EndpointRequest(node string, r coordinator.UpdateNodeEndpointRequest) *wire.UpdateNodeEndpointRequest {
	ttl, _ := time.ParseDuration(r.TTL) // Validated against the durable request before dispatch.
	value := &wire.UpdateNodeEndpointRequest{NodeId: node, Endpoint: r.Endpoint, Generation: r.Generation, Candidates: r.Candidates,
		Ttl: durationpb.New(ttl), Status: r.Status, ClientVersion: r.ClientVersion}
	if a := r.Transport; a != nil {
		value.Transport = &wire.TransportAdvertisement{IntentGeneration: a.IntentGeneration,
			Capabilities: &wire.TransportCapabilities{Version: a.Capabilities.Version}}
		for _, e := range a.Capabilities.Engines {
			value.Transport.Capabilities.Engines = append(value.Transport.Capabilities.Engines, &wire.TransportCapability{
				Engine: e.Engine, ProtocolVersion: e.ProtocolVersion, ProfileVersion: e.ProfileVersion,
			})
		}
		for _, e := range a.Endpoints {
			value.Transport.Endpoints = append(value.Transport.Endpoints, &wire.TransportEndpoint{
				ProfileId: e.ProfileID, ProfileDigest: e.ProfileDigest, Endpoint: e.Endpoint,
			})
		}
	}
	return value
}

func EndpointReceipt(m *wire.UpdateNodeEndpointResponse) (coordinator.UpdateNodeEndpointResponse, error) {
	if !knownRPC(m.ProtoReflect()) || m.Revision == nil {
		return coordinator.UpdateNodeEndpointResponse{}, errMapShape
	}
	d := mapDecoder{}
	value := coordinator.UpdateNodeEndpointResponse{SchemaVersion: coordinator.SchemaVersion,
		NodeID: m.NodeId, NetworkID: m.NetworkId, Endpoint: m.Endpoint, Generation: m.Generation, Online: m.Online,
		UpdatedAt: d.timestamp(m.UpdatedAt), ExpiresAt: d.timestamp(m.ExpiresAt), Revision: MapRevision(m.Revision), Transport: d.advertisement(m.Transport),
	}
	return value, d.err
}

func MapUpdate(m *wire.WatchMapsResponse) (coordinator.MapStreamEvent, error) {
	if !knownRPC(m.ProtoReflect()) || m.From == nil || m.EventId == "" {
		return coordinator.MapStreamEvent{}, errMapShape
	}
	event := coordinator.MapStreamEvent{EventID: m.EventId, From: MapRevision(m.From)}
	var snapshot *wire.NetworkMapSnapshot
	switch update := m.Update.(type) {
	case *wire.WatchMapsResponse_Snapshot:
		event.Type, snapshot = "snapshot", update.Snapshot
	case *wire.WatchMapsResponse_Resync:
		event.Type, snapshot = "resync", update.Resync
	default:
		return coordinator.MapStreamEvent{}, errMapShape
	}
	value, err := MapSnapshot(snapshot)
	if err != nil {
		return coordinator.MapStreamEvent{}, err
	}
	event.Snapshot, event.To, event.ResultSignature = &value, value.Revision, value.MapSignature
	if proof := m.TimeProof; proof != nil {
		if proof.ServerTime.CheckValid() != nil {
			return coordinator.MapStreamEvent{}, errMapShape
		}
		event.TimeProof = &coordinator.MapTimeProof{Nonce: proof.Nonce, MapHash: proof.MapHash, ServerTime: proof.ServerTime.AsTime(), Signature: proof.Signature}
	}
	return event, nil
}
