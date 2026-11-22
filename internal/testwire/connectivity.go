// Package testwire provides generated messages for signed domain fixtures.
package testwire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/http"

	"github.com/endless-net/client/api/transport"
	contract "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Update(event contract.MapStreamEvent) *wire.WatchMapsResponse {
	message := &wire.WatchMapsResponse{EventId: event.EventID, From: wireRevision(event.From)}
	if event.Snapshot != nil {
		switch event.Type {
		case "snapshot":
			message.Update = &wire.WatchMapsResponse_Snapshot{Snapshot: Map(*event.Snapshot)}
		case "resync":
			message.Update = &wire.WatchMapsResponse_Resync{Resync: Map(*event.Snapshot)}
		}
	}
	if p := event.TimeProof; p != nil {
		message.TimeProof = &wire.MapTimeProof{Nonce: p.Nonce, MapHash: p.MapHash, ServerTime: timestamppb.New(p.ServerTime), Signature: p.Signature}
	}
	return message
}

func Frame(message proto.Message) ([]byte, error) {
	body, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 5, len(body)+5)
	binary.BigEndian.PutUint32(frame[1:], uint32(len(body)))
	return append(frame, body...), nil
}

func ReadWatch(request *http.Request) (*wire.WatchMapsRequest, error) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 65537))
	if err != nil {
		return nil, err
	}
	if len(raw) < 5 || len(raw) > 65536 || raw[0] != 0 || int(binary.BigEndian.Uint32(raw[1:5])) != len(raw)-5 ||
		request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/connect+proto" {
		return nil, errors.New("invalid Connect fixture request")
	}
	value := &wire.WatchMapsRequest{}
	return value, proto.Unmarshal(raw[5:], value)
}

func EndpointReceipt(r contract.UpdateNodeEndpointResponse) *wire.UpdateNodeEndpointResponse {
	return &wire.UpdateNodeEndpointResponse{NodeId: r.NodeID, NetworkId: r.NetworkID, Endpoint: r.Endpoint,
		Generation: r.Generation, Online: r.Online, UpdatedAt: timestamppb.New(r.UpdatedAt), ExpiresAt: timestamppb.New(r.ExpiresAt),
		Revision: wireRevision(r.Revision), Transport: wireAdvertisement(r.Transport)}
}

func EndpointRequest(raw []byte) (contract.UpdateNodeEndpointRequest, error) {
	m := &wire.UpdateNodeEndpointRequest{}
	if err := proto.Unmarshal(raw, m); err != nil {
		return contract.UpdateNodeEndpointRequest{}, err
	}
	if m.Ttl.CheckValid() != nil {
		return contract.UpdateNodeEndpointRequest{}, errors.New("missing endpoint TTL")
	}
	r := contract.UpdateNodeEndpointRequest{Endpoint: m.Endpoint, Generation: m.Generation, Candidates: m.Candidates,
		TTL: m.Ttl.AsDuration().String(), Status: m.Status, ClientVersion: m.ClientVersion}
	if a := m.Transport; a != nil {
		r.Transport = &contract.TransportAdvertisement{IntentGeneration: a.IntentGeneration, Endpoints: []contract.TransportEndpoint{}}
		if c := a.Capabilities; c != nil {
			r.Transport.Capabilities.Version = c.Version
			for _, e := range c.Engines {
				r.Transport.Capabilities.Engines = append(r.Transport.Capabilities.Engines, clientCapability(e))
			}
		}
		for _, e := range a.Endpoints {
			r.Transport.Endpoints = append(r.Transport.Endpoints, contract.TransportEndpoint{ProfileID: e.ProfileId, ProfileDigest: e.ProfileDigest, Endpoint: e.Endpoint})
		}
	}
	return r, nil
}

func UnaryResponse(request *http.Request, message proto.Message) (*http.Response, error) {
	body, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Request: request, Header: http.Header{"Content-Type": {"application/proto"}},
		Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func StreamResponse(request *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: 200, Request: request, Header: http.Header{"Content-Type": {"application/connect+proto"}}, Body: body}
}

func clientCapability(e *wire.TransportCapability) transport.Capability {
	return transport.Capability{Engine: e.Engine, ProtocolVersion: e.ProtocolVersion, ProfileVersion: e.ProfileVersion}
}

func RenewalRequest(raw []byte) (contract.RegisterNodeRequest, error) {
	m := &wire.RegisterNodeRequest{}
	if err := proto.Unmarshal(raw, m); err != nil {
		return contract.RegisterNodeRequest{}, err
	}
	p, r := m.Proof, m.GetRenewal()
	if p == nil || r == nil {
		return contract.RegisterNodeRequest{}, errors.New("missing renewal proof")
	}
	return contract.RegisterNodeRequest{
		SchemaVersion: int(p.Version), IdempotencyID: p.IdempotencyId, NetworkID: p.NetworkId, NetworkName: p.NetworkName,
		AccountID: p.AccountId, CellID: p.CellId, Hostname: p.Hostname, ClientVersion: p.ClientVersion,
		IdentityPublicKey: p.IdentityPublicKey, IdentitySignature: p.IdentitySignature, PublicKey: p.PublicKey,
		DeviceFingerprint: p.DeviceFingerprint, Endpoint: p.Endpoint, EndpointGeneration: p.EndpointGeneration,
		EndpointCandidates: p.EndpointCandidates, EndpointTTL: p.EndpointTtl, AdvertisedIPs: p.AdvertisedIps, Tags: p.Tags,
		NodeCredential: r.NodeCredential, RegistrationBinding: r.RegistrationBinding, SessionTokenBinding: r.SessionTokenBinding,
	}, nil
}

func RenewalReceipt(r contract.NodeCredentialRenewalResponse) *wire.RegisterNodeResponse {
	return &wire.RegisterNodeResponse{Result: &wire.RegisterNodeResponse_Renewal{Renewal: &wire.NodeCredentialRenewalReceipt{
		IdempotencyId: r.IdempotencyID, RenewalBinding: r.RenewalBinding, RegistrationBinding: r.RegistrationBinding,
		PreviousCredentialSha256: r.PreviousCredentialSHA256, NetworkId: r.NetworkID, NodeId: r.NodeID, NodeCredential: r.NodeCredential,
		IssuedAt: timestamppb.New(r.IssuedAt), ExpiresAt: timestamppb.New(r.ExpiresAt),
	}}}
}

func DeviceProof(r contract.RegisterNodeRequest) *wire.DeviceRegistrationProof {
	return &wire.DeviceRegistrationProof{Version: uint32(r.SchemaVersion), IdempotencyId: r.IdempotencyID,
		NetworkId: r.NetworkID, NetworkName: r.NetworkName, AccountId: r.AccountID, CellId: r.CellID, Hostname: r.Hostname,
		ClientVersion: r.ClientVersion, IdentityPublicKey: r.IdentityPublicKey, IdentitySignature: r.IdentitySignature,
		PublicKey: r.PublicKey, DeviceFingerprint: r.DeviceFingerprint, Endpoint: r.Endpoint, EndpointGeneration: r.EndpointGeneration,
		EndpointCandidates: r.EndpointCandidates, EndpointTtl: r.EndpointTTL, AdvertisedIps: r.AdvertisedIPs, Tags: r.Tags}
}

func Registration(r contract.RegisterNodeResponse) *wire.NodeRegistration {
	return &wire.NodeRegistration{IdempotencyId: r.IdempotencyID, NodeCredential: r.NodeCredential,
		Map: Map(contract.NetworkMapSnapshot{Revision: r.Revision, Network: r.Network, Node: r.Node, Peers: r.Peers,
			RegistrationBinding: r.RegistrationBinding, STUNEndpoints: r.STUNEndpoints, Relays: r.Relays,
			RelayCredential: r.RelayCredential, MapSignature: r.MapSignature})}
}

func EnrollmentView(r contract.NodeEnrollmentRequest) *wire.NodeEnrollmentRequest {
	return &wire.NodeEnrollmentRequest{Id: r.ID, Status: r.Status, AccountId: r.AccountID, NetworkId: r.NetworkID, NetworkName: r.NetworkName,
		Hostname: r.Hostname, PublicKey: r.PublicKey, IdentityPublicKey: r.IdentityPublicKey, DeviceFingerprint: r.DeviceFingerprint, Tags: r.Tags,
		ApprovalUrl: r.ApprovalURL, ApprovedByUserId: r.ApprovedByUserID, RejectedByUserId: r.RejectedByUserID, RejectionReason: r.RejectionReason,
		NodeId: r.NodeID, CreatedAt: timestamppb.New(r.CreatedAt), ExpiresAt: timestamppb.New(r.ExpiresAt),
		ApprovedAt: optionalWireTime(r.ApprovedAt), RejectedAt: optionalWireTime(r.RejectedAt), CompletedAt: optionalWireTime(r.CompletedAt)}
}
