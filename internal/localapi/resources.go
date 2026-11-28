package localapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"slices"
	"strings"
	"time"
)

func (c *Controller) ListResources(ctx context.Context, req *clientv0.ListResourcesRequest) (*clientv0.ListResourcesResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.listResources(ctx, caller, req)
}

func (c *Controller) listResources(ctx context.Context, caller localipc.Principal, req *clientv0.ListResourcesRequest) (*clientv0.ListResourcesResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	saved, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(saved)
	if err := owner(&saved, caller, false); err != nil {
		return nil, err
	}
	p := saved.Profiles[saved.ActiveProfileID]
	if p.ID == "" || p.ID != profileID(req.GetProfile(), saved.ActiveProfileID) {
		return nil, status.Error(codes.NotFound, "active profile required")
	}
	size := req.GetPage().GetPageSize()
	if size == 0 {
		size = 100
	}
	if size > 500 {
		return nil, status.Error(codes.InvalidArgument, "page size exceeds 500")
	}
	search := strings.ToLower(strings.TrimSpace(req.GetSearch()))
	kinds := slices.Clone(req.GetKinds())
	slices.Sort(kinds)
	kinds = slices.Compact(kinds)
	for _, k := range kinds {
		if k < clientv0.ResourceKind_RESOURCE_KIND_HOST || k > clientv0.ResourceKind_RESOURCE_KIND_APPLICATION {
			return nil, status.Error(codes.InvalidArgument, "unknown resource kind")
		}
	}
	result := &clientv0.ListResourcesResponse{Page: &clientv0.PageResponse{Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: saved.Revision, GeneratedAt: timestamppb.New(c.config.Now())}}}
	var snapshot connectivity.Observation
	if c.config.ConnectionObservations != nil {
		snapshot = c.config.ConnectionObservations.ConnectionSnapshot()
	}
	conn, local := p.Connection, snapshot.Local
	if conn == nil || conn.Intent != state.IntentConnected || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || snapshot.Profile != p.ID || snapshot.Generation != conn.Generation || snapshot.Intent != conn.Intent || snapshot.Phase != connectivity.PhaseConnected || !local.Known || !local.Applied || !local.Permission.NewFlows || local.Permission.Revoked || local.Permission.ClockUnsafe || local.MapRevision == 0 || !observedMapMatchesTarget(local, conn.MapCursor) || local.Resources == nil {
		if req.GetPage().GetPageToken() != "" {
			return nil, rpcError(state.ErrStale)
		}
		return nil, status.Error(codes.Unavailable, "current resource observation unavailable")
	}
	digest := sha256.New()
	fmt.Fprintf(digest, "client-resource-page/v1 %q %q %q %q %d %d %q %d %q %v ", c.config.RuntimeID, caller.SID, p.ID, p.Attempt.NetworkID, conn.Generation, local.MapRevision, local.MapPayloadHash, size, search, kinds)
	var rows []*clientv0.Resource
	for _, r := range local.Resources {
		if r.GetId() == "" || r.NetworkId != p.Attempt.NetworkID {
			return nil, status.Error(codes.Unavailable, "invalid resource observation")
		}
		if len(kinds) > 0 && !slices.Contains(kinds, r.Kind) {
			continue
		}
		fields := []string{r.Id, r.DisplayName, r.GetSubnet().GetCidr(), r.GetHost().GetHostname(), strings.Join(r.GetHost().GetAddresses(), " "), r.GetApplication().GetDisplayAddress(), r.GetService().GetHostname()}
		if !slices.ContainsFunc(fields, func(v string) bool { return strings.Contains(strings.ToLower(v), search) }) {
			continue
		}
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "invalid resource")
		}
		fmt.Fprintf(digest, "%d:", len(data))
		digest.Write(data)
		rows = append(rows, r)
	}
	signature := func(offset []byte) []byte {
		h := hmac.New(sha256.New, c.config.LogPageKey[:])
		h.Write(digest.Sum(nil))
		h.Write(offset)
		return h.Sum(nil)
	}
	start := uint32(0)
	if token := req.GetPage().GetPageToken(); token != "" {
		if len(token) != base64.RawURLEncoding.EncodedLen(4+sha256.Size) || c.config.LogPageKey == [32]byte{} {
			return nil, rpcError(state.ErrStale)
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || len(data) != 4+sha256.Size || !hmac.Equal(data[4:], signature(data[:4])) {
			return nil, rpcError(state.ErrStale)
		}
		start = binary.BigEndian.Uint32(data[:4])
		if uint64(start) >= uint64(len(rows)) {
			return nil, rpcError(state.ErrStale)
		}
	}
	budget := min(c.config.LogResponseBytes, 32<<10) - proto.Size(result) - 64
	for _, r := range rows[start:] {
		n := proto.Size(r) + 5
		if len(result.Resources) == int(size) || budget < n {
			if len(result.Resources) == 0 || c.config.LogPageKey == [32]byte{} {
				return nil, status.Error(codes.ResourceExhausted, "resource page exceeds response budget")
			}
			offset := binary.BigEndian.AppendUint32(nil, start+uint32(len(result.Resources)))
			result.Page.NextPageToken = base64.RawURLEncoding.EncodeToString(append(offset, signature(offset)...))
			break
		}
		result.Resources = append(result.Resources, proto.Clone(r).(*clientv0.Resource))
		budget -= n
	}
	return result, nil
}
