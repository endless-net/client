package localapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) ListPeers(ctx context.Context, req *clientv0.ListPeersRequest) (*clientv0.ListPeersResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.listPeers(ctx, caller, req)
}

func (c *Controller) listPeers(ctx context.Context, caller localipc.Principal, req *clientv0.ListPeersRequest) (*clientv0.ListPeersResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err := owner(&s, caller, false); err != nil {
		return nil, err
	}
	profile := s.Profiles[s.ActiveProfileID]
	if profile.ID == "" || profileID(req.GetProfile(), s.ActiveProfileID) != profile.ID {
		return nil, status.Error(codes.NotFound, "active peer profile required")
	}
	page := req.GetPage()
	size := page.GetPageSize()
	if size == 0 {
		size = 100
	}
	if size > 500 {
		return nil, status.Error(codes.InvalidArgument, "page size exceeds 500")
	}
	result := &clientv0.ListPeersResponse{
		SnapshotState: clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_ABSENT,
		Page: &clientv0.PageResponse{Metadata: &clientv0.SnapshotMetadata{
			InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now()),
		}},
	}
	if !currentPeerObservation(profile, c.config.ConnectionObservations, result) {
		if page.GetPageToken() != "" {
			return nil, rpcError(state.ErrStale)
		}
		return result, nil
	}
	return c.pagePeers(result, caller, profile, size, page.GetPageToken(), strings.ToLower(strings.TrimSpace(req.GetSearch())))
}

func currentPeerObservation(profile state.Profile, source connectivity.ObservationSource, result *clientv0.ListPeersResponse) bool {
	connection := profile.Connection
	if source == nil || connection == nil || connection.Intent != state.IntentConnected ||
		profile.Attempt == nil || profile.Attempt.Phase != enrollment.Registered {
		return false
	}
	if connection.MapCursor != nil {
		result.TargetMapRevision = connection.MapCursor.Revision.Network
	}
	v := source.ConnectionSnapshot()
	local := v.Local
	if v.Profile != profile.ID || v.Generation != connection.Generation || v.Intent != connection.Intent ||
		local.Permission.Revoked || local.Permission.ClockUnsafe {
		return false
	}
	if last := connection.LastAppliedMap; last != nil {
		result.MapRevision = last.Revision.Network
		result.SnapshotState = clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
	}
	if v.Phase != connectivity.PhaseConnected || !local.Known || !local.Applied || local.MapRevision == 0 {
		return false
	}
	result.MapRevision = local.MapRevision
	result.SnapshotState = clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS
	if !local.Permission.NewFlows || !observedMapMatchesTarget(local, connection.MapCursor) || len(local.Peers) != int(local.PeerCount) {
		return false
	}
	result.SnapshotState = clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT
	// The caller only receives the selected transport observation, never map keys
	// or remote underlay endpoints.
	transports := make(map[string]*clientv0.PeerTransportStatus, len(local.PeerTransports))
	for _, transport := range local.PeerTransports {
		transports[transport.GetPeerId()] = transport
	}
	for _, peer := range local.Peers {
		peer = proto.Clone(peer).(*clientv0.Peer)
		if transport := transports[peer.Id]; transport != nil {
			peer.Transport = proto.Clone(transport).(*clientv0.PeerTransportStatus)
			peer.SelectedPath = transport.GetSelectedPath()
		}
		result.Peers = append(result.Peers, peer)
	}
	return true
}

func (c *Controller) pagePeers(result *clientv0.ListPeersResponse, caller localipc.Principal, profile state.Profile, size uint32, token, search string) (*clientv0.ListPeersResponse, error) {
	peers := result.Peers
	result.Peers = nil
	sign := func(payload []byte) []byte {
		mac := hmac.New(sha256.New, c.config.LogPageKey[:])
		_, _ = fmt.Fprintf(mac, "client-peer-page/v1 %q %q %q %q %d %d %d %q ", c.config.RuntimeID, caller.SID,
			profile.ID, profile.Attempt.NetworkID, profile.Connection.Generation, result.MapRevision, size, search)
		// Bind positions to the exact ordered membership even if a map is
		// reissued at the same network revision.
		for _, peer := range peers {
			_, _ = fmt.Fprintf(mac, "%q %q %q ", peer.Id, peer.Hostname, peer.OverlayAddresses)
		}
		_, _ = mac.Write(payload)
		return mac.Sum(nil)
	}
	var offset uint32
	if token != "" {
		if c.config.LogPageKey == [32]byte{} || len(token) != base64.RawURLEncoding.EncodedLen(4+sha256.Size) {
			return nil, rpcError(state.ErrStale)
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || !hmac.Equal(sign(data[:4]), data[4:]) {
			return nil, rpcError(state.ErrStale)
		}
		offset = binary.BigEndian.Uint32(data[:4])
	}
	skip := offset
	remaining := min(c.config.LogResponseBytes, 32<<10) - proto.Size(result) - 64
	for _, peer := range peers {
		if search != "" && !strings.Contains(strings.ToLower(peer.Hostname), search) && !strings.Contains(strings.ToLower(peer.Id), search) &&
			!strings.Contains(strings.Join(peer.OverlayAddresses, " "), search) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		bytes := proto.Size(peer) + 5
		if uint32(len(result.Peers)) == size || bytes > remaining {
			if len(result.Peers) == 0 || c.config.LogPageKey == [32]byte{} {
				return nil, status.Error(codes.ResourceExhausted, "peer page exceeds response budget")
			}
			var cursor [4]byte
			binary.BigEndian.PutUint32(cursor[:], offset+uint32(len(result.Peers)))
			result.Page.NextPageToken = base64.RawURLEncoding.EncodeToString(append(cursor[:], sign(cursor[:])...))
			break
		}
		result.Peers = append(result.Peers, peer)
		remaining -= bytes
	}
	return result, nil
}
