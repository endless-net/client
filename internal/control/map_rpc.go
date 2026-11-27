package control

import (
	"errors"
	"math"
	"time"

	clienttransport "github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	relay "github.com/endless-net/relay/api/wire"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errMapShape = errors.New("invalid Coordinator map message")

// MapSnapshot restores the canonical data used by the published map signature.
func MapSnapshot(m *wire.NetworkMapSnapshot) (coordinator.NetworkMapSnapshot, error) {
	if !knownRPC(m.ProtoReflect()) || m.Revision == nil || m.Network == nil || m.Node == nil {
		return coordinator.NetworkMapSnapshot{}, errMapShape
	}
	d := mapDecoder{}
	value := coordinator.NetworkMapSnapshot{
		Revision:            MapRevision(m.Revision),
		Network:             d.network(m.Network),
		Node:                d.node(m.Node),
		RegistrationBinding: m.RegistrationBinding,
		Peers:               decodeSlice(m.Peers, d.peer),
		STUNEndpoints: decodeSlice(m.StunEndpoints, func(e *wire.MapSTUNEndpoint) coordinator.STUNEndpoint {
			return coordinator.STUNEndpoint{ID: e.Id, Addr: e.Addr}
		}),
		Relays: decodeSlice(m.Relays, func(e *wire.MapRelayEndpoint) relay.Endpoint {
			return relay.Endpoint{ID: e.Id, Addr: e.Addr, Protocol: e.Protocol, Region: e.Region, Priority: int(e.Priority)}
		}),
	}
	if c := m.RelayCredential; c != nil {
		value.RelayCredential = &relay.Credential{Algorithm: c.Algorithm, KeyID: c.KeyId, NetworkID: c.NetworkId, NodeID: c.NodeId, ExpiresAt: d.timestamp(c.ExpiresAt), Signature: c.Signature}
	}
	if s := m.MapSignature; s != nil {
		value.MapSignature = &coordinator.MapSignature{Version: int(s.Version), KeyID: s.KeyId, Algorithm: s.Algorithm, PayloadHash: s.PayloadHash, IssuedAt: d.timestamp(s.IssuedAt), ExpiresAt: d.timestamp(s.ExpiresAt), Signature: s.Signature}
	}
	return value, d.err
}

func MapRevision(m *wire.MapRevision) coordinator.MapRevision {
	return coordinator.MapRevision{Network: m.GetNetwork(), Global: m.GetGlobal()}
}

type mapDecoder struct{ err error }

func (d *mapDecoder) timestamp(t *timestamppb.Timestamp) time.Time {
	if t.CheckValid() != nil {
		d.err = errMapShape
		return time.Time{}
	}
	return t.AsTime()
}

func (d *mapDecoder) optionalTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := d.timestamp(t)
	return &value
}

func decodeSlice[W, D any](values []W, decode func(W) D) []D {
	if len(values) == 0 {
		return nil
	}
	result := make([]D, len(values))
	for i, value := range values {
		result[i] = decode(value)
	}
	return result
}

func (d *mapDecoder) network(n *wire.MapNetwork) coordinator.Network {
	value := coordinator.Network{
		ID: n.Id, Name: n.Name, CIDR: n.Cidr, IPv6CIDR: n.Ipv6Cidr, DNS: n.Dns,
		CellID: n.CellId, AuthoritativeCellID: n.AuthoritativeCellId, MigratingToCellID: n.MigratingToCellId,
		Revision: n.Revision, OwnerID: n.OwnerId, AccountID: n.AccountId, CreatedAt: d.timestamp(n.CreatedAt),
		Applications: decodeSlice(n.Applications, d.application), Services: decodeSlice(n.Services, decodeService),
		SharePeerGrants: decodeSlice(n.SharePeerGrants, d.shareGrant),
	}
	if v := n.DnsConfig; v != nil {
		value.DNSConfig = &coordinator.DNSConfig{MagicDNSEnabled: v.MagicdnsEnabled, HTTPSCertsEnabled: v.HttpsCertsEnabled,
			OverrideLocalDNS: v.OverrideLocalDns, Suffix: v.Suffix, SearchDomains: v.SearchDomains,
			Zones: decodeSlice(v.Zones, func(z *wire.MapDNSZone) coordinator.DNSZone {
				return coordinator.DNSZone{Name: z.GetName(), Version: z.GetVersion(), Records: decodeSlice(z.GetRecords(), func(r *wire.MapDNSRecord) coordinator.DNSRecord {
					return coordinator.DNSRecord{Name: r.GetName(), Type: r.GetType(), Value: r.GetValue(), TTL: r.GetTtl()}
				})}
			}),
			Nameservers: decodeSlice(v.Nameservers, func(s *wire.MapDNSNameserver) coordinator.DNSNameserver {
				return coordinator.DNSNameserver{ID: s.Id, Address: s.Address, Scope: s.Scope, Priority: s.Priority, SplitDomains: s.SplitDomains}
			}),
		}
	}
	if v := n.ClientPolicy; v != nil {
		value.ClientPolicy = d.clientPolicy(v)
	}
	return value
}

func (d *mapDecoder) clientPolicy(p *wire.MapClientPolicy) *coordinator.ClientPolicy {
	return &coordinator.ClientPolicy{
		ExitNodes: decodeSlice(p.ExitNodes, func(g *wire.MapExitNodeGrant) coordinator.ExitNodeGrant {
			return coordinator.ExitNodeGrant{ID: g.Id, Name: g.Name, Host: decodeServiceHost(g.Host), ExpiresAt: d.timestamp(g.ExpiresAt),
				AllowedFamilyModes: decodeSlice(g.AllowedFamilyModes, func(v string) coordinator.ExitFamilyMode { return coordinator.ExitFamilyMode(v) }),
				AllowedLANAccess:   decodeSlice(g.AllowedLanAccess, func(v string) coordinator.ExitLANAccess { return coordinator.ExitLANAccess(v) }),
			}
		}),
		Settings: decodeSlice(p.Settings, func(s *wire.MapManagedClientSetting) coordinator.ManagedClientSetting {
			result := coordinator.ManagedClientSetting{Key: coordinator.ClientSettingKey(s.Key), Source: coordinator.ClientPolicySource(s.Source), PolicyID: s.PolicyId, Locked: s.Locked}
			switch v := s.Value.(type) {
			case *wire.MapManagedClientSetting_BooleanValue:
				result.BooleanValue = &v.BooleanValue
			case *wire.MapManagedClientSetting_LifecycleValue:
				value := coordinator.ClientLifecycleBehavior(v.LifecycleValue)
				result.LifecycleValue = &value
			}
			return result
		}),
		Resources: decodeSlice(p.Resources, func(s *wire.MapManagedResourceSetting) coordinator.ManagedResourceSetting {
			return coordinator.ManagedResourceSetting{Kind: coordinator.ManagedResourceKind(s.Kind), ID: s.Id, CIDR: s.Cidr,
				Source: coordinator.ClientPolicySource(s.Source), PolicyID: s.PolicyId, Locked: s.Locked, Enabled: s.Enabled}
		}),
	}
}

func decodeServiceHost(h *wire.MapServiceHost) coordinator.ServiceHost {
	return coordinator.ServiceHost{NodeID: h.GetNodeId(), PublicKey: h.GetPublicKey()}
}

func (d *mapDecoder) application(a *wire.MapApplication) coordinator.Application {
	return coordinator.Application{ID: a.Id, Name: a.Name, Target: a.Target, TargetType: a.TargetType, DNSEnabled: a.DnsEnabled, PolicyHash: a.PolicyHash,
		Sources: decodeSlice(a.Sources, decodeServiceHost), Connectors: decodeSlice(a.Connectors, decodeServiceHost),
		Routes: decodeSlice(a.Routes, func(r *wire.MapApplicationRoute) coordinator.ApplicationRoute {
			return coordinator.ApplicationRoute{Connector: decodeServiceHost(r.Connector), CIDRs: r.Cidrs, ExpiresAt: d.timestamp(r.ExpiresAt)}
		}),
	}
}

func decodeService(s *wire.MapAdvertisedService) coordinator.AdvertisedService {
	return coordinator.AdvertisedService{ID: s.Id, Name: s.Name, Description: s.Description, DNSName: s.DnsName, Tags: s.Tags, EligibleTags: s.EligibleTags,
		ApprovalMode: s.ApprovalMode, ApprovalStatus: s.ApprovalStatus, Health: s.Health, Hosts: decodeSlice(s.Hosts, decodeServiceHost),
		Ports: decodeSlice(s.Ports, func(p *wire.MapServicePort) coordinator.ServicePort {
			return coordinator.ServicePort{Protocol: p.Protocol, Port: p.Port}
		}),
	}
}

func (d *mapDecoder) shareGrant(g *wire.MapSharePeerGrant) coordinator.SharePeerGrant {
	return coordinator.SharePeerGrant{GrantID: g.GrantId, RecipientNetworkID: g.RecipientNetworkId, RecipientNodeID: g.RecipientNodeId,
		RecipientPublicKey: g.RecipientPublicKey, RecipientAllowedIPs: g.RecipientAllowedIps, SourceNetworkID: g.SourceNetworkId, SourceNodeID: g.SourceNodeId,
		SourcePublicKey: g.SourcePublicKey, SourceAllowedIPs: g.SourceAllowedIps, Initiation: g.Initiation, Revision: g.Revision,
		IssuedAt: d.timestamp(g.IssuedAt), ExpiresAt: d.timestamp(g.ExpiresAt),
		Rights: decodeSlice(g.Rights, func(r *wire.MapShareTraffic) coordinator.ShareTraffic {
			return coordinator.ShareTraffic{Protocol: r.Protocol, FirstPort: r.FirstPort, LastPort: r.LastPort}
		}),
	}
}
func (d *mapDecoder) node(n *wire.MapNode) coordinator.Node {
	return coordinator.Node{
		ID: n.Id, NetworkID: n.NetworkId, UserID: n.UserId, Hostname: n.Hostname, IdentityPublicKey: n.IdentityPublicKey,
		PublicKey: n.PublicKey, DeviceFingerprint: n.DeviceFingerprint, AssignedIP: n.AssignedIp, AssignedIPv6: n.AssignedIpv6,
		Endpoint: n.Endpoint, EndpointGeneration: n.EndpointGeneration, EndpointCandidates: n.EndpointCandidates,
		EndpointExpiresAt: d.optionalTime(n.EndpointExpiresAt), Transport: d.advertisement(n.Transport), Status: n.Status,
		AdvertisedIPs: n.AdvertisedIps, RequestedTags: n.RequestedTags, Tags: n.Tags, ApprovalState: n.ApprovalState,
		Ephemeral: n.Ephemeral, KeyExpiryEnabled: n.KeyExpiryEnabled, KeyExpiresAt: d.optionalTime(n.KeyExpiresAt),
		CreatedAt: d.timestamp(n.CreatedAt), UpdatedAt: d.timestamp(n.UpdatedAt), LastSeen: d.timestamp(n.LastSeen),
	}
}

func (d *mapDecoder) peer(p *wire.MapPeer) coordinator.Peer {
	return coordinator.Peer{
		ID: p.Id, NetworkID: p.NetworkId, Hostname: p.Hostname, PublicKey: p.PublicKey, Endpoint: p.Endpoint,
		EndpointGeneration: p.EndpointGeneration, EndpointCandidates: p.EndpointCandidates, EndpointExpiresAt: d.optionalTime(p.EndpointExpiresAt),
		Status: p.Status, AllowedIPs: p.AllowedIps, AllowedPorts: decodeSlice(p.AllowedPorts, decodeACLPort), ACLRestricted: p.AclRestricted,
		ACLGrants: decodeSlice(p.AclGrants, decodeACLGrant), IngressACLGrants: decodeSlice(p.IngressAclGrants, decodeACLGrant),
		Tags: p.Tags, Transport: d.peerTransport(p.Transport),
	}
}

func decodeACLPort(p *wire.MapACLPort) coordinator.ACLPort {
	return coordinator.ACLPort{Protocol: p.Protocol, Port: int(p.Port)}
}

func decodeACLGrant(g *wire.MapACLGrant) coordinator.ACLGrant {
	return coordinator.ACLGrant{DestinationCIDRs: g.DestinationCidrs, AllowedPorts: decodeSlice(g.AllowedPorts, decodeACLPort)}
}

func (d *mapDecoder) advertisement(a *wire.TransportAdvertisement) *coordinator.TransportAdvertisement {
	if a == nil {
		return nil
	}
	if a.Capabilities == nil {
		d.err = errMapShape
		return nil
	}
	return &coordinator.TransportAdvertisement{
		IntentGeneration: a.IntentGeneration,
		Capabilities: clienttransport.Capabilities{Version: a.Capabilities.Version,
			Engines: decodeSlice(a.Capabilities.Engines, func(e *wire.TransportCapability) clienttransport.Capability {
				return clienttransport.Capability{Engine: e.Engine, ProtocolVersion: e.ProtocolVersion, ProfileVersion: e.ProfileVersion}
			}),
		},
		Endpoints: append([]coordinator.TransportEndpoint{}, decodeSlice(a.Endpoints, func(e *wire.TransportEndpoint) coordinator.TransportEndpoint {
			return coordinator.TransportEndpoint{ProfileID: e.ProfileId, ProfileDigest: e.ProfileDigest, Endpoint: e.Endpoint}
		})...),
	}
}

func (d *mapDecoder) peerTransport(p *wire.PeerTransportPlan) *coordinator.PeerTransportPlan {
	if p == nil {
		return nil
	}
	advertisement := d.advertisement(p.PeerAdvertisement)
	if p.Revision == nil || advertisement == nil {
		d.err = errMapShape
		return nil
	}
	profiles := make([]coordinator.TransportProfileOffer, 0, len(p.Profiles))
	for _, o := range p.Profiles {
		profiles = append(profiles, coordinator.TransportProfileOffer{Profile: d.transportProfile(o.Profile), Digest: o.Digest, LocalEndpoints: append([]string{}, o.LocalEndpoints...), PeerEndpoints: append([]string{}, o.PeerEndpoints...)})
	}
	return &coordinator.PeerTransportPlan{
		Version: p.Version, Availability: coordinator.TransportAvailability(p.Availability), AccountID: p.AccountId, NetworkID: p.NetworkId,
		LocalNodeID: p.LocalNodeId, LocalPublicKey: p.LocalPublicKey, PeerNodeID: p.PeerNodeId, PeerPublicKey: p.PeerPublicKey,
		LocalProbeIPv4: p.LocalProbeIpv4, PeerProbeIPv4: p.PeerProbeIpv4,
		LocalIntentGeneration: p.LocalIntentGeneration, Revision: MapRevision(p.Revision), IssuedAt: d.timestamp(p.IssuedAt),
		ExpiresAt: d.timestamp(p.ExpiresAt), PeerAdvertisement: *advertisement, Profiles: profiles,
	}
}

func (d *mapDecoder) uint16(value uint32) uint16 {
	if value > math.MaxUint16 {
		d.err = errMapShape
	}
	return uint16(value)
}

func (d *mapDecoder) transportProfile(p *wire.TransportProfile) clienttransport.Profile {
	if p == nil {
		d.err = errMapShape
		return clienttransport.Profile{}
	}
	value := clienttransport.Profile{Version: p.Version, ID: p.Id, Revision: p.Revision, Engine: p.Engine, ProtocolVersion: p.ProtocolVersion,
		Prelude: make([]clienttransport.Datagram, 0, len(p.Prelude))}
	for _, datagram := range p.Prelude {
		value.Prelude = append(value.Prelude, clienttransport.Datagram{DelayMS: d.uint16(datagram.DelayMs),
			Segments: decodeSlice(datagram.Segments, func(s *wire.TransportSegment) clienttransport.Segment {
				return clienttransport.Segment{Bytes: s.Bytes, Random: d.uint16(s.Random)}
			}),
		})
	}
	if a := p.Awg; a != nil {
		if a.ContentPadding == nil {
			d.err = errMapShape
			return value
		}
		value.AWG = &clienttransport.AWG{Headers: decodeSlice(a.Headers, decodeTransportRange), Padding: decodeSlice(a.Padding, d.uint16),
			HeaderProtectionKey: a.HeaderProtectionKey, ContentPadding: decodeTransportRange(a.ContentPadding),
			RandomTrailers: a.RandomTrailers, DisableCookies: a.DisableCookies}
	}
	return value
}

func decodeTransportRange(r *wire.TransportRange) clienttransport.Range {
	return clienttransport.Range{Min: r.Min, Max: r.Max}
}

func registrationResult(r *wire.NodeRegistration) (coordinator.RegisterNodeResponse, error) {
	if !knownRPC(r.ProtoReflect()) {
		return coordinator.RegisterNodeResponse{}, ErrRegistrationProtocol
	}
	m, err := MapSnapshot(r.Map)
	if err != nil {
		return coordinator.RegisterNodeResponse{}, ErrRegistrationProtocol
	}
	return coordinator.RegisterNodeResponse{
		SchemaVersion: coordinator.SchemaVersion, IdempotencyID: r.IdempotencyId, NodeCredential: r.NodeCredential,
		Revision: m.Revision, Network: m.Network, Node: m.Node, Peers: m.Peers, RegistrationBinding: m.RegistrationBinding,
		STUNEndpoints: m.STUNEndpoints, Relays: m.Relays, RelayCredential: m.RelayCredential, MapSignature: m.MapSignature,
	}, nil
}
