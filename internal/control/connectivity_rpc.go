package control

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	clienttransport "github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func wireTransportProfile(p clienttransport.Profile) *wire.TransportProfile {
	response := &wire.TransportProfile{
		Version: p.Version, Id: p.ID, Revision: p.Revision, Engine: p.Engine, ProtocolVersion: p.ProtocolVersion,
		Prelude: decodeSlice(p.Prelude, func(d clienttransport.Datagram) *wire.TransportDatagram {
			return &wire.TransportDatagram{
				DelayMs: uint32(d.DelayMS),
				Segments: decodeSlice(d.Segments, func(s clienttransport.Segment) *wire.TransportSegment {
					return &wire.TransportSegment{Bytes: s.Bytes, Random: uint32(s.Random)}
				}),
			}
		}),
	}
	if a := p.AWG; a != nil {
		response.Awg = &wire.TransportAmneziaWG{
			Headers: decodeSlice(a.Headers, wireTransportRange), Padding: decodeSlice(a.Padding, func(v uint16) uint32 { return uint32(v) }),
			HeaderProtectionKey: a.HeaderProtectionKey, ContentPadding: wireTransportRange(a.ContentPadding),
			RandomTrailers: a.RandomTrailers, DisableCookies: a.DisableCookies,
		}
	}
	return response
}

func wireTransportRange(r clienttransport.Range) *wire.TransportRange {
	return &wire.TransportRange{Min: r.Min, Max: r.Max}
}

func EndpointRequest(node string, r coordinator.UpdateNodeEndpointRequest) *wire.UpdateNodeEndpointRequest {
	ttl, _ := time.ParseDuration(r.TTL) // Validated against the durable request before dispatch.
	value := &wire.UpdateNodeEndpointRequest{NodeId: node, Endpoint: r.Endpoint, Generation: r.Generation, Candidates: r.Candidates, AdvertisedCidrs: r.AdvertisedCIDRs,
		Ttl: durationpb.New(ttl), Status: r.Status, ClientVersion: r.ClientVersion}
	if m := r.Posture; m != nil {
		value.Posture = &wire.NodePostureMetadata{ObservedAt: timestamppb.New(m.ObservedAt), Platform: m.Platform, OsVersion: m.OSVersion, KernelVersion: m.KernelVersion, HardwareSerial: m.HardwareSerial, LocalAddresses: m.LocalAddresses, Processes: m.Processes}
		if cert := m.Certificate; cert != nil {
			value.Posture.Certificate = &wire.CertificatePossession{Challenge: cert.Challenge, ChainDer: cert.ChainDER, Signature: cert.Signature}
		}
	}
	if a := r.Transport; a != nil {
		value.Transport = &wire.TransportAdvertisement{IntentGeneration: a.IntentGeneration,
			Capabilities: &wire.TransportCapabilities{Version: a.Capabilities.Version}}
		value.Transport.ProposedProfiles = decodeSlice(a.ProposedProfiles, wireTransportProfile)
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
	value.CertificateChallenge = m.CertificateChallenge
	if p := m.ProcessProbes; p != nil {
		value.ProcessProbes = &coordinator.ProcessProbePlan{Platform: p.Platform, ExecutablePaths: p.ExecutablePaths}
		if !value.ProcessProbes.Valid(m.Online) {
			return coordinator.UpdateNodeEndpointResponse{}, errMapShape
		}
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
	case *wire.WatchMapsResponse_Heartbeat:
		if update.Heartbeat == nil || m.TimeProof != nil {
			return coordinator.MapStreamEvent{}, errMapShape
		}
		event.Type, event.To = "heartbeat", event.From
		return event, nil
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
