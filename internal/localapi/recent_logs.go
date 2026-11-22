package localapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RecentLog struct {
	Sequence uint64
	Time     time.Time
	Message  string
}

func (c *Controller) ListRecentLogs(ctx context.Context, req *clientv0.ListRecentLogsRequest) (*clientv0.ListRecentLogsResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.listRecentLogs(ctx, caller, req)
}

func (c *Controller) listRecentLogs(ctx context.Context, caller localipc.Principal, req *clientv0.ListRecentLogsRequest) (*clientv0.ListRecentLogsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err := owner(&s, caller, false); err != nil {
		return nil, rpcError(err)
	}
	id := profileID(req.GetProfile(), s.ActiveProfileID)
	if id == "" || id != s.ActiveProfileID {
		return nil, status.Error(codes.NotFound, "active log profile required")
	}
	if c.config.RecentLogs == nil {
		return nil, status.Error(codes.Unimplemented, "client logs unavailable")
	}
	page := req.GetPage()
	size := page.GetPageSize()
	if size == 0 {
		size = 100
	}
	if size > 500 {
		return nil, status.Error(codes.InvalidArgument, "page size exceeds 500")
	}
	sign := func(payload []byte) []byte {
		mac := hmac.New(sha256.New, c.config.LogPageKey[:])
		_, _ = fmt.Fprintf(mac, "client-log-page/v1 %q %q %q %d %d ", c.config.RuntimeID, caller.SID, id, s.Revision, size)
		_, _ = mac.Write(payload)
		return mac.Sum(nil)
	}
	logs := c.config.RecentLogs()
	var after, end uint64
	if len(logs) != 0 {
		end = logs[len(logs)-1].Sequence
	}
	if token := page.GetPageToken(); token != "" {
		if len(token) != 64 {
			return nil, rpcError(state.ErrStale)
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || len(data) != 48 || !hmac.Equal(sign(data[:16]), data[16:]) {
			return nil, rpcError(state.ErrStale)
		}
		after, end = binary.BigEndian.Uint64(data[:8]), binary.BigEndian.Uint64(data[8:16])
		if len(logs) == 0 || after < logs[0].Sequence-1 || end > logs[len(logs)-1].Sequence {
			return nil, rpcError(state.ErrStale)
		}
	}

	result := &clientv0.ListRecentLogsResponse{Page: &clientv0.PageResponse{
		Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())},
	}}
	// Reserve space for the cursor and cap wire size independently of page size.
	remaining := min(c.config.LogResponseBytes, 32<<10) - proto.Size(result) - 70
	for _, log := range logs {
		if log.Sequence <= after || log.Sequence > end {
			continue
		}
		entry := &clientv0.LogEntry{Timestamp: timestamppb.New(log.Time), Message: log.Message}
		bytes := proto.Size(entry) + 5
		if uint32(len(result.Logs)) == size || bytes > remaining {
			if len(result.Logs) == 0 {
				return nil, status.Error(codes.ResourceExhausted, "log entry exceeds response budget")
			}
			var cursor [16]byte
			binary.BigEndian.PutUint64(cursor[:8], after)
			binary.BigEndian.PutUint64(cursor[8:], end)
			result.Page.NextPageToken = base64.RawURLEncoding.EncodeToString(append(cursor[:], sign(cursor[:])...))
			break
		}
		result.Logs = append(result.Logs, entry)
		remaining -= bytes
		after = log.Sequence
	}
	return result, nil
}
