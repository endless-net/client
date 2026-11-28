package connectivity

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	contract "github.com/endless-net/coordinator/api/enrollment"
	api "github.com/endless-net/management/api/v1"
	"google.golang.org/protobuf/proto"
)

var ErrPublication = errors.New("publication route unavailable")

// ErrPublicationTarget identifies a known publication without an eligible backend
// after the caller's source admission has passed.
var ErrPublicationTarget = errors.New("no eligible publication target")

// PublicationRoutes is scoped to one registered proxy. It starts empty on every
// process start. An installed generation is replaced as one complete set.
type PublicationRoutes struct {
	mu            sync.RWMutex
	executor      *api.PublicationExecutor
	maps          *Lease
	now           func() time.Time
	snapshot      *api.GetPublicationRoutesResponse
	routes        map[string]*api.PublicationRoute
	targetOffset  map[string]int
	generationCtx context.Context
	cancel        context.CancelFunc
}

func NewPublicationRoutes(e *api.PublicationExecutor, maps *Lease, now func() time.Time) (*PublicationRoutes, error) {
	if !identifier(e.GetAccountId()) || !identifier(e.GetNetworkId()) || !identifier(e.GetProxyId()) || !identifier(e.GetNodeId()) || maps == nil || now == nil {
		return nil, ErrConfig
	}
	return &PublicationRoutes{executor: proto.CloneOf(e), maps: maps, now: now}, nil
}

func validPublicationPeers(peers []*api.PublicationPeer) bool {
	seen := map[string]bool{}
	for _, p := range peers {
		if p == nil || len(p.ProtoReflect().GetUnknown()) != 0 || !identifier(p.GetNodeId()) || !wireGuardKey(p.GetPublicKey()) || seen[p.NodeId] {
			return false
		}
		seen[p.NodeId] = true
	}
	return true
}

func (p *PublicationRoutes) Install(s *api.GetPublicationRoutesResponse) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if s == nil || proto.Size(s) > 1<<20 || len(s.ProtoReflect().GetUnknown()) != 0 || !proto.Equal(p.executor, s.GetExecutor()) || s.GetGeneration() == 0 || !identifier(s.GetLeaseId()) || s.GetIssuedAt().CheckValid() != nil || s.GetValidUntil().CheckValid() != nil || now.Before(s.IssuedAt.AsTime()) || !now.Before(s.ValidUntil.AsTime()) || s.Generation < p.snapshot.GetGeneration() {
		return ErrPublication
	}
	if s.Generation == p.snapshot.GetGeneration() {
		if proto.Equal(s, p.snapshot) {
			return nil
		}
		return ErrPublication
	}
	copy := proto.CloneOf(s)
	routes := make(map[string]*api.PublicationRoute, len(copy.Routes))
	services := map[string]bool{}
	for _, r := range copy.Routes {
		host, ok := dnsDomain(r.GetHostname())
		_, ipErr := netip.ParseAddr(host)
		if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || !ok || !strings.Contains(host, ".") || ipErr == nil || r.Hostname != host || !identifier(r.ServiceId) || services[r.ServiceId] || routes[host] != nil || r.Version == 0 || r.TargetPort == 0 || r.TargetPort > 65535 || r.DomainValidUntil.CheckValid() != nil || !r.DomainValidUntil.AsTime().After(s.IssuedAt.AsTime()) || !validPublicationPeers(r.Targets) || !validPublicationPeers(r.AllowedSources) {
			return ErrPublication
		}
		switch r.Access {
		case api.PublicationAccess_PUBLICATION_ACCESS_PUBLIC:
			if len(r.AllowedSources) != 0 {
				return ErrPublication
			}
		case api.PublicationAccess_PUBLICATION_ACCESS_NETWORK_GROUPS:
		default:
			return ErrPublication
		}
		services[r.ServiceId] = true
		routes[host] = r
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.generationCtx, p.cancel = context.WithCancel(context.Background())
	nextOffsets := make(map[string]int, len(routes))
	for host, offset := range p.targetOffset {
		if old, next := p.routes[host], routes[host]; old != nil && next != nil && old.ServiceId == next.ServiceId && old.Version == next.Version && slices.EqualFunc(old.Targets, next.Targets, func(a, b *api.PublicationPeer) bool { return proto.Equal(a, b) }) {
			nextOffsets[host] = offset
		}
	}
	p.routes, p.targetOffset = routes, nextOffsets
	p.snapshot = copy
	return nil
}

// PublicationTarget contains only the address and name from the verified peer
// map. Generation is cancelled immediately when the snapshot is replaced.
type PublicationTarget struct {
	Peer       contract.ServiceHost
	Address    netip.AddrPort
	TLSName    string
	HTTPS      bool
	Until      time.Time
	Generation context.Context
}

// Resolve checks both authorities for every admission. An optional source must
// be the peer authenticated by the overlay transport handling this connection.
func (p *PublicationRoutes) Resolve(host string, source *api.PublicationPeer) (PublicationTarget, error) {
	return p.resolve(host, source, false)
}

// AllowsCertificate authorizes certificate selection and issuance for an exact
// live route. HTTP requests must additionally authorize their source via Resolve.
func (p *PublicationRoutes) AllowsCertificate(host string) bool {
	_, err := p.ProbeTarget(host)
	return err == nil
}

// ProbeTarget resolves a backend for the executor's transport check.
// Forwarding a user request requires Resolve with the actual source.
func (p *PublicationRoutes) ProbeTarget(host string) (PublicationTarget, error) {
	return p.resolve(host, nil, true)
}

func (p *PublicationRoutes) resolve(host string, source *api.PublicationPeer, executorCheck bool) (PublicationTarget, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.resolveLocked(host, source, executorCheck)
}

// AdvanceProbe rotates the preferred backend only if the failed target still
// belongs to the installed generation. Every candidate goes through resolve.
func (p *PublicationRoutes) AdvanceProbe(host string, failed PublicationTarget) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	current, err := p.resolveLocked(host, nil, true)
	if err != nil || current != failed {
		return false
	}
	r := p.routes[host]
	for i, peer := range r.Targets {
		if peer.NodeId == failed.Peer.NodeID && peer.PublicKey == failed.Peer.PublicKey {
			p.targetOffset[host] = (i + 1) % len(r.Targets)
			next, err := p.resolveLocked(host, nil, true)
			return err == nil && next.Peer != failed.Peer
		}
	}
	return false
}

func (p *PublicationRoutes) resolveLocked(host string, source *api.PublicationPeer, executorCheck bool) (PublicationTarget, error) {
	r := p.routes[host]
	if r == nil || !p.maps.Permit(false) {
		return PublicationTarget{}, ErrPublication
	}
	m := p.maps.CurrentMap()
	snap := m.snapshot
	now := p.now()
	if m.denied || snap.MapSignature == nil || snap.Network.AccountID != p.executor.AccountId || snap.Network.ID != p.executor.NetworkId || snap.Node.ID != p.executor.NodeId || now.Before(p.snapshot.IssuedAt.AsTime()) || !now.Before(p.snapshot.ValidUntil.AsTime()) || !now.Before(r.DomainValidUntil.AsTime()) {
		return PublicationTarget{}, ErrPublication
	}
	if !executorCheck && r.Access == api.PublicationAccess_PUBLICATION_ACCESS_NETWORK_GROUPS {
		matches := func(a *api.PublicationPeer) bool {
			return source != nil && source.NodeId == a.NodeId && source.PublicKey == a.PublicKey
		}
		if !slices.ContainsFunc(r.AllowedSources, matches) || !slices.ContainsFunc(snap.Peers, func(peer contract.Peer) bool {
			return peer.NetworkID == snap.Network.ID && peer.ID == source.NodeId && peer.PublicKey == source.PublicKey
		}) {
			return PublicationTarget{}, ErrPublication
		}
	}
	authorities := m.servicePeers
	port := contract.ACLPort{Protocol: "tcp", Port: int(r.TargetPort)}
	for _, svc := range snap.Network.Services {
		if svc.ID != r.ServiceId || !slices.ContainsFunc(svc.Ports, func(v contract.ServicePort) bool { return v.Protocol == "tcp" && int(v.Port) == port.Port }) {
			continue
		}
		for i := range r.Targets {
			peer := r.Targets[(i+p.targetOffset[host])%len(r.Targets)]
			h := contract.ServiceHost{NodeID: peer.NodeId, PublicKey: peer.PublicKey}
			a, ok := authorities[h]
			if !ok || !slices.Contains(svc.Hosts, h) {
				continue
			}
			end := a.ends[port]
			if all := a.ends[contract.ACLPort{Protocol: contract.ProtocolAll}]; all.After(end) {
				end = all
			}
			for _, limit := range []time.Time{p.snapshot.ValidUntil.AsTime(), r.DomainValidUntil.AsTime()} {
				if limit.Before(end) {
					end = limit
				}
			}
			if !p.maps.GrantDeadlineCurrent(m, end) || !p.now().Before(end) || !p.maps.MatchesMap(m) || !p.maps.Permit(false) {
				continue
			}
			name, _ := dnsDomain(svc.DNSName)
			return PublicationTarget{h, netip.AddrPortFrom(netip.AddrFrom4(a.ip), uint16(r.TargetPort)), name, r.TargetHttps, end, p.generationCtx}, nil
		}
	}
	return PublicationTarget{}, ErrPublicationTarget
}

// CertificateNames lists exact candidate names in the installed snapshot.
// AllowsCertificate rechecks the lease, map and target before issuance or use.
func (p *PublicationRoutes) CertificateNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.routes))
	for name := range p.routes {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (p *PublicationRoutes) Executor() *api.PublicationExecutor { return proto.CloneOf(p.executor) }
