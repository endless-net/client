package testwire

import (
	"time"

	"github.com/endless-net/client/api/transport"
	contract "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	relayauth "github.com/endless-net/relay/api/wire"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Map(m contract.NetworkMapSnapshot) *wire.NetworkMapSnapshot {
	response := &wire.NetworkMapSnapshot{
		Revision: wireRevision(m.Revision), Network: wireNetwork(m.Network), Node: wireNode(m.Node),
		RegistrationBinding: m.RegistrationBinding, Peers: mapWireSlice(m.Peers, wirePeer),
		StunEndpoints: mapWireSlice(m.STUNEndpoints, func(e contract.STUNEndpoint) *wire.MapSTUNEndpoint {
			return &wire.MapSTUNEndpoint{Id: e.ID, Addr: e.Addr}
		}),
		Relays: mapWireSlice(m.Relays, func(e relayauth.Endpoint) *wire.MapRelayEndpoint {
			return &wire.MapRelayEndpoint{Id: e.ID, Addr: e.Addr, Protocol: e.Protocol, Region: e.Region, Priority: int32(e.Priority)}
		}),
	}
	if c := m.RelayCredential; c != nil {
		response.RelayCredential = &wire.MapRelayCredential{
			Algorithm: c.Algorithm, KeyId: c.KeyID, NetworkId: c.NetworkID, NodeId: c.NodeID,
			ExpiresAt: timestamppb.New(c.ExpiresAt), Signature: c.Signature,
		}
	}
	if s := m.MapSignature; s != nil {
		response.MapSignature = &wire.MapSignature{
			Version: uint32(s.Version), KeyId: s.KeyID, Algorithm: s.Algorithm, PayloadHash: s.PayloadHash,
			IssuedAt: timestamppb.New(s.IssuedAt), ExpiresAt: timestamppb.New(s.ExpiresAt), Signature: s.Signature,
		}
	}
	return response
}

func wireRevision(r contract.MapRevision) *wire.MapRevision {
	return &wire.MapRevision{Network: r.Network, Global: r.Global}
}

func wireNetwork(n contract.Network) *wire.MapNetwork {
	response := &wire.MapNetwork{
		Id: n.ID, Name: n.Name, Cidr: n.CIDR, Ipv6Cidr: n.IPv6CIDR, Dns: n.DNS,
		CellId: n.CellID, AuthoritativeCellId: n.AuthoritativeCellID, MigratingToCellId: n.MigratingToCellID,
		Revision: n.Revision, OwnerId: n.OwnerID, AccountId: n.AccountID, CreatedAt: timestamppb.New(n.CreatedAt),
		Applications:    mapWireSlice(n.Applications, wireApplication),
		Services:        mapWireSlice(n.Services, wireService),
		SharePeerGrants: mapWireSlice(n.SharePeerGrants, wireShareGrant),
	}
	if d := n.DNSConfig; d != nil {
		response.DnsConfig = &wire.MapDNSConfig{
			MagicdnsEnabled: d.MagicDNSEnabled, HttpsCertsEnabled: d.HTTPSCertsEnabled,
			OverrideLocalDns: d.OverrideLocalDNS, Suffix: d.Suffix, SearchDomains: d.SearchDomains,
			Zones: mapWireSlice(d.Zones, func(z contract.DNSZone) *wire.MapDNSZone {
				return &wire.MapDNSZone{Name: z.Name, Version: z.Version, Records: mapWireSlice(z.Records, func(r contract.DNSRecord) *wire.MapDNSRecord {
					return &wire.MapDNSRecord{Name: r.Name, Type: r.Type, Value: r.Value, Ttl: r.TTL}
				})}
			}),
			Nameservers: mapWireSlice(d.Nameservers, func(s contract.DNSNameserver) *wire.MapDNSNameserver {
				return &wire.MapDNSNameserver{Id: s.ID, Address: s.Address, Scope: s.Scope, Priority: s.Priority, SplitDomains: s.SplitDomains}
			}),
		}
	}
	if p := n.ClientPolicy; p != nil {
		response.ClientPolicy = wireClientPolicy(p)
	}
	return response
}

func wireClientPolicy(p *contract.ClientPolicy) *wire.MapClientPolicy {
	return &wire.MapClientPolicy{
		ExitNodes: mapWireSlice(p.ExitNodes, func(g contract.ExitNodeGrant) *wire.MapExitNodeGrant {
			return &wire.MapExitNodeGrant{
				Id: g.ID, Name: g.Name, Host: wireServiceHost(g.Host), ExpiresAt: timestamppb.New(g.ExpiresAt),
				AllowedFamilyModes: mapWireSlice(g.AllowedFamilyModes, func(v contract.ExitFamilyMode) string { return string(v) }),
				AllowedLanAccess:   mapWireSlice(g.AllowedLANAccess, func(v contract.ExitLANAccess) string { return string(v) }),
			}
		}),
		Settings: mapWireSlice(p.Settings, func(s contract.ManagedClientSetting) *wire.MapManagedClientSetting {
			value := &wire.MapManagedClientSetting{Key: string(s.Key), Source: string(s.Source), PolicyId: s.PolicyID, Locked: s.Locked}
			switch {
			case s.BooleanValue != nil:
				value.Value = &wire.MapManagedClientSetting_BooleanValue{BooleanValue: *s.BooleanValue}
			case s.LifecycleValue != nil:
				value.Value = &wire.MapManagedClientSetting_LifecycleValue{LifecycleValue: string(*s.LifecycleValue)}
			}
			return value
		}),
		Resources: mapWireSlice(p.Resources, func(s contract.ManagedResourceSetting) *wire.MapManagedResourceSetting {
			return &wire.MapManagedResourceSetting{
				Kind: string(s.Kind), Id: s.ID, Cidr: s.CIDR, Source: string(s.Source), PolicyId: s.PolicyID,
				Locked: s.Locked, Enabled: s.Enabled,
			}
		}),
	}
}

func wireServiceHost(h contract.ServiceHost) *wire.MapServiceHost {
	return &wire.MapServiceHost{NodeId: h.NodeID, PublicKey: h.PublicKey}
}

func wireApplication(a contract.Application) *wire.MapApplication {
	return &wire.MapApplication{
		Id: a.ID, Name: a.Name, Target: a.Target, TargetType: a.TargetType, DnsEnabled: a.DNSEnabled, PolicyHash: a.PolicyHash,
		Sources: mapWireSlice(a.Sources, wireServiceHost), Connectors: mapWireSlice(a.Connectors, wireServiceHost),
		Routes: mapWireSlice(a.Routes, func(r contract.ApplicationRoute) *wire.MapApplicationRoute {
			return &wire.MapApplicationRoute{Connector: wireServiceHost(r.Connector), Cidrs: r.CIDRs, ExpiresAt: timestamppb.New(r.ExpiresAt)}
		}),
	}
}

func wireService(s contract.AdvertisedService) *wire.MapAdvertisedService {
	return &wire.MapAdvertisedService{
		Id: s.ID, Name: s.Name, Description: s.Description, DnsName: s.DNSName, Tags: s.Tags, EligibleTags: s.EligibleTags,
		ApprovalMode: s.ApprovalMode, ApprovalStatus: s.ApprovalStatus, Health: s.Health, Hosts: mapWireSlice(s.Hosts, wireServiceHost),
		Ports: mapWireSlice(s.Ports, func(p contract.ServicePort) *wire.MapServicePort {
			return &wire.MapServicePort{Protocol: p.Protocol, Port: p.Port}
		}),
	}
}

func wireShareGrant(g contract.SharePeerGrant) *wire.MapSharePeerGrant {
	return &wire.MapSharePeerGrant{
		GrantId: g.GrantID, RecipientNetworkId: g.RecipientNetworkID, RecipientNodeId: g.RecipientNodeID,
		RecipientPublicKey: g.RecipientPublicKey, RecipientAllowedIps: g.RecipientAllowedIPs,
		SourceNetworkId: g.SourceNetworkID, SourceNodeId: g.SourceNodeID,
		SourcePublicKey: g.SourcePublicKey, SourceAllowedIps: g.SourceAllowedIPs,
		Initiation: g.Initiation, Revision: g.Revision, IssuedAt: timestamppb.New(g.IssuedAt), ExpiresAt: timestamppb.New(g.ExpiresAt),
		Rights: mapWireSlice(g.Rights, func(r contract.ShareTraffic) *wire.MapShareTraffic {
			return &wire.MapShareTraffic{Protocol: r.Protocol, FirstPort: r.FirstPort, LastPort: r.LastPort}
		}),
	}
}

func wirePeer(p contract.Peer) *wire.MapPeer {
	response := &wire.MapPeer{
		Id: p.ID, NetworkId: p.NetworkID, Hostname: p.Hostname, PublicKey: p.PublicKey, Endpoint: p.Endpoint,
		EndpointGeneration: p.EndpointGeneration, EndpointCandidates: p.EndpointCandidates, EndpointExpiresAt: optionalWireTime(p.EndpointExpiresAt),
		Status: p.Status, AllowedIps: p.AllowedIPs, AllowedPorts: mapWireSlice(p.AllowedPorts, wireACLPort), AclRestricted: p.ACLRestricted,
		AclGrants: mapWireSlice(p.ACLGrants, wireACLGrant), IngressAclGrants: mapWireSlice(p.IngressACLGrants, wireACLGrant), Tags: p.Tags,
	}
	if t := p.Transport; t != nil {
		response.Transport = &wire.PeerTransportPlan{
			Version: t.Version, Availability: string(t.Availability), AccountId: t.AccountID, NetworkId: t.NetworkID,
			LocalNodeId: t.LocalNodeID, LocalPublicKey: t.LocalPublicKey, PeerNodeId: t.PeerNodeID, PeerPublicKey: t.PeerPublicKey,
			LocalProbeIpv4: t.LocalProbeIPv4, PeerProbeIpv4: t.PeerProbeIPv4,
			LocalIntentGeneration: t.LocalIntentGeneration, Revision: wireRevision(t.Revision),
			IssuedAt: timestamppb.New(t.IssuedAt), ExpiresAt: timestamppb.New(t.ExpiresAt), PeerAdvertisement: wireAdvertisement(&t.PeerAdvertisement),
			Profiles: mapWireSlice(t.Profiles, func(o contract.TransportProfileOffer) *wire.TransportProfileOffer {
				return &wire.TransportProfileOffer{Profile: wireTransportProfile(o.Profile), Digest: o.Digest, LocalEndpoints: o.LocalEndpoints, PeerEndpoints: o.PeerEndpoints}
			}),
		}
	}
	return response
}

func wireACLPort(p contract.ACLPort) *wire.MapACLPort {
	return &wire.MapACLPort{Protocol: p.Protocol, Port: uint32(p.Port)}
}

func wireACLGrant(g contract.ACLGrant) *wire.MapACLGrant {
	return &wire.MapACLGrant{DestinationCidrs: g.DestinationCIDRs, AllowedPorts: mapWireSlice(g.AllowedPorts, wireACLPort)}
}

func wireTransportProfile(p transport.Profile) *wire.TransportProfile {
	response := &wire.TransportProfile{
		Version: p.Version, Id: p.ID, Revision: p.Revision, Engine: p.Engine, ProtocolVersion: p.ProtocolVersion,
		Prelude: mapWireSlice(p.Prelude, func(d transport.Datagram) *wire.TransportDatagram {
			return &wire.TransportDatagram{
				DelayMs: uint32(d.DelayMS),
				Segments: mapWireSlice(d.Segments, func(s transport.Segment) *wire.TransportSegment {
					return &wire.TransportSegment{Bytes: s.Bytes, Random: uint32(s.Random)}
				}),
			}
		}),
	}
	if a := p.AWG; a != nil {
		response.Awg = &wire.TransportAmneziaWG{
			Headers: mapWireSlice(a.Headers, wireTransportRange), Padding: mapWireSlice(a.Padding, func(v uint16) uint32 { return uint32(v) }),
			HeaderProtectionKey: a.HeaderProtectionKey, ContentPadding: wireTransportRange(a.ContentPadding),
			RandomTrailers: a.RandomTrailers, DisableCookies: a.DisableCookies,
		}
	}
	return response
}

func wireTransportRange(r transport.Range) *wire.TransportRange {
	return &wire.TransportRange{Min: r.Min, Max: r.Max}
}

func mapWireSlice[Domain, Wire any](values []Domain, convert func(Domain) Wire) []Wire {
	if len(values) == 0 {
		return nil
	}
	result := make([]Wire, len(values))
	for i, value := range values {
		result[i] = convert(value)
	}
	return result
}
func wireNode(node contract.Node) *wire.MapNode {
	return &wire.MapNode{
		Id: node.ID, NetworkId: node.NetworkID, UserId: node.UserID, Hostname: node.Hostname,
		IdentityPublicKey: node.IdentityPublicKey, PublicKey: node.PublicKey, DeviceFingerprint: node.DeviceFingerprint,
		AssignedIp: node.AssignedIP, AssignedIpv6: node.AssignedIPv6, Endpoint: node.Endpoint,
		EndpointGeneration: node.EndpointGeneration, EndpointCandidates: node.EndpointCandidates,
		EndpointExpiresAt: optionalWireTime(node.EndpointExpiresAt), Transport: wireAdvertisement(node.Transport),
		Status: node.Status, AdvertisedIps: node.AdvertisedIPs, RequestedTags: node.RequestedTags, Tags: node.Tags,
		ApprovalState: node.ApprovalState, Ephemeral: node.Ephemeral, KeyExpiryEnabled: node.KeyExpiryEnabled,
		KeyExpiresAt: optionalWireTime(node.KeyExpiresAt), CreatedAt: timestamppb.New(node.CreatedAt),
		UpdatedAt: timestamppb.New(node.UpdatedAt), LastSeen: timestamppb.New(node.LastSeen),
	}
}

func optionalWireTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func wireAdvertisement(a *contract.TransportAdvertisement) *wire.TransportAdvertisement {
	if a == nil {
		return nil
	}
	result := &wire.TransportAdvertisement{
		IntentGeneration: a.IntentGeneration,
		Capabilities:     &wire.TransportCapabilities{Version: a.Capabilities.Version},
	}
	result.ProposedProfiles = mapWireSlice(a.ProposedProfiles, wireTransportProfile)
	for _, engine := range a.Capabilities.Engines {
		result.Capabilities.Engines = append(result.Capabilities.Engines, &wire.TransportCapability{
			Engine: engine.Engine, ProtocolVersion: engine.ProtocolVersion, ProfileVersion: engine.ProfileVersion,
		})
	}
	for _, endpoint := range a.Endpoints {
		result.Endpoints = append(result.Endpoints, &wire.TransportEndpoint{
			ProfileId: endpoint.ProfileID, ProfileDigest: endpoint.ProfileDigest, Endpoint: endpoint.Endpoint,
		})
	}
	return result
}
