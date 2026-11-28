package control

import (
	"connectrpc.com/connect"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (r *Registration) rpc(origin string) publicqueryv1connect.EnrollmentServiceClient {
	return publicqueryv1connect.NewEnrollmentServiceClient(r.http, origin,
		connect.WithSendMaxBytes(r.config.MaxRequestBytes), connect.WithReadMaxBytes(r.config.MaxResponseBytes))
}

func bearer[T any](message *T, token string) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set("Authorization", "Bearer "+token)
	return request
}

func registrationProof(r coordinator.RegisterNodeRequest) *wire.DeviceRegistrationProof {
	return &wire.DeviceRegistrationProof{
		Version: uint32(r.SchemaVersion), IdempotencyId: r.IdempotencyID, NetworkId: r.NetworkID,
		NetworkName: r.NetworkName, AccountId: r.AccountID, CellId: r.CellID, Hostname: r.Hostname,
		ClientVersion: r.ClientVersion, IdentityPublicKey: r.IdentityPublicKey, IdentitySignature: r.IdentitySignature,
		PublicKey: r.PublicKey, DeviceFingerprint: r.DeviceFingerprint, Endpoint: r.Endpoint,
		EndpointGeneration: r.EndpointGeneration, EndpointCandidates: r.EndpointCandidates, EndpointTtl: r.EndpointTTL,
		AdvertisedIps: r.AdvertisedIPs, Tags: r.Tags,
	}
}

func knownRPC(m protoreflect.Message) bool {
	if !m.IsValid() || len(m.GetUnknown()) != 0 {
		return false
	}
	valid := true
	m.Range(func(f protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if f.IsMap() {
			if f.MapValue().Message() != nil {
				value.Map().Range(func(_ protoreflect.MapKey, entry protoreflect.Value) bool {
					valid = knownRPC(entry.Message())
					return valid
				})
			}
			return valid
		}
		if f.Message() == nil {
			return true
		}
		if f.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if !knownRPC(list.Get(i).Message()) {
					valid = false
					return false
				}
			}
			return true
		}
		valid = knownRPC(value.Message())
		return valid
	})
	return valid
}
