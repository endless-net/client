package dataplane

import (
	"sort"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// publishTransportStatus runs on the actor. Readers receive detached public
// producer messages, never live candidate objects or parameter documents.
func (g *transportSupervisor) publishTransportStatus(now connectivity.ClockSample) {
	choices := g.outputChoices()
	primary := make(map[string]DirectPath)
	paths, _, _ := g.primary.direct.snapshot()
	for _, path := range paths {
		primary[path.PeerID] = path
	}
	result := make([]*clientv0.PeerTransportStatus, 0, len(g.snapshot.Peers))
	for _, peer := range g.snapshot.Peers {
		v := &clientv0.PeerTransportStatus{PeerId: peer.ID, SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_IDLE, ReasonKey: "client.transport.awaiting_path"}
		p := g.peers[peer.ID]
		choice, overridden := choices[peer.ID]
		if p != nil {
			switch choice {
			case peerRouteRelay:
				v.SelectedPath = clientv0.PathKind_PATH_KIND_RELAY
				v.Engine, v.ProtocolVersion = "wireguard", "1"
				v.ReasonKey = "client.transport.relay"
				if !p.checkedRelay.Wall.IsZero() {
					v.CheckedAt = timestamppb.New(p.checkedRelay.Wall)
				}
			case peerRouteDirect:
				if p.direct != nil {
					p.direct.bind.mu.Lock()
					profile := p.direct.bind.assignment.offer.Profile
					p.direct.bind.mu.Unlock()
					v.SelectedPath = clientv0.PathKind_PATH_KIND_DIRECT
					v.Engine, v.ProtocolVersion = profile.Engine, profile.ProtocolVersion
					v.TransportProfileId, v.TransportProfileRevision = profile.ID, profile.Revision
					v.ReasonKey = "client.transport.direct"
					if !p.checkedDirect.Wall.IsZero() {
						v.CheckedAt = timestamppb.New(p.checkedDirect.Wall)
					}
				}
			}
			switch {
			case p.search != nil && !p.search.finished:
				v.SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_SEARCHING
			case p.retryBlocked:
				v.SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_EXHAUSTED
				v.ReasonKey = "client.transport.clock_unsafe"
			case p.cooldown != nil:
				if delay, err := p.cooldown.Remaining(); err == nil {
					v.SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_BACKOFF
					v.NextSearchAt = timestamppb.New(now.Wall.Add(delay))
				}
			}
		}
		if path := primary[peer.ID]; !overridden && path.Authenticated && g.lease.Permissions().ExistingFlows {
			v.SelectedPath = clientv0.PathKind_PATH_KIND_DIRECT
			v.Engine, v.ProtocolVersion = "wireguard", "1"
			v.ReasonKey = "client.transport.direct"
			v.CheckedAt = timestamppb.New(path.ConfirmedAt)
		}
		if peer.Transport == nil || len(peer.Transport.Profiles) == 0 {
			v.SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_EXHAUSTED
			v.ReasonKey = "client.transport.no_profiles"
			v.NextSearchAt = nil
			if peer.Transport != nil && peer.Transport.Availability == coordinator.TransportUnsupported {
				v.SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_UNSUPPORTED
				v.ReasonKey = "client.transport.capabilities_incompatible"
			}
		}
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PeerId < result[j].PeerId })
	g.mu.Lock()
	g.status, g.statusCursor = result, g.current
	g.mu.Unlock()
}

func (g *transportSupervisor) transportStatuses(value connectivity.Map) []*clientv0.PeerTransportStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.statusCursor.Cursor() != value.Cursor() || !g.lease.MatchesMap(value) {
		return nil
	}
	result := make([]*clientv0.PeerTransportStatus, len(g.status))
	for i, v := range g.status {
		result[i] = proto.Clone(v).(*clientv0.PeerTransportStatus)
		if !g.lease.Permissions().ExistingFlows {
			result[i].SelectedPath = clientv0.PathKind_PATH_KIND_UNSPECIFIED
			result[i].Engine, result[i].ProtocolVersion, result[i].TransportProfileId = "", "", ""
			result[i].TransportProfileRevision = 0
			result[i].CheckedAt, result[i].NextSearchAt = nil, nil
			result[i].SearchState = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_IDLE
			result[i].ReasonKey = "client.transport.paused"
		}
	}
	return result
}
