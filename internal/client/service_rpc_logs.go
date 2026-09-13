package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ClientRPCRecentLogsProvider returns an owned, stable, oldest-first snapshot
// for exactly the requested profile. It must not return a global agent buffer.
// No credentials/configuration cross this boundary. Configure before serving.
type ClientRPCRecentLogsProvider func(context.Context, string) ([]*ipc.LogEntry, error)

const (
	maxRPCRecentLogs      = 500
	maxRPCLogMessageBytes = 4096
)

func (s *ClientRPCService) ListRecentLogs(ctx context.Context, request *connect.Request[ipc.ListRecentLogsRequest]) (*connect.Response[ipc.ListRecentLogsResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.recentLogsAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) recentLogsAs(ctx context.Context, peer local.Peer, request *ipc.ListRecentLogsRequest) (*ipc.ListRecentLogsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/ListRecentLogs")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	if request.GetPage().GetPageSize() > 500 || len(request.GetPage().GetPageToken()) > 2048 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if s.RecentLogsProvider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	entries, err := s.RecentLogsProvider(ctx, profile.ID)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	// No truncation field exists on this response: do not silently discard logs.
	if len(entries) > maxRPCRecentLogs {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	logs := make([]*ipc.LogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Timestamp == nil || entry.Timestamp.CheckValid() != nil || !utf8.ValidString(entry.Message) {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		if len(logs) > 0 && entry.Timestamp.AsTime().Before(logs[len(logs)-1].Timestamp.AsTime()) {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		message := strings.TrimSpace(RedactServiceLogMessage(entry.Message))
		if len(message) > maxRPCLogMessageBytes {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		// Construct known fields only; unknown provider fields are not diagnostics.
		logs = append(logs, &ipc.LogEntry{Timestamp: timestamppb.New(entry.Timestamp.AsTime()), Message: message})
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&ipc.ListRecentLogsResponse{Logs: logs})
	if err != nil {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	digest := sha256.Sum256(encoded)
	// Config revision alone cannot detect buffer append/rotation. Bind the cursor
	// to the redacted snapshot too, rather than skipping/repeating changed entries.
	scope := "ListRecentLogs\x00" + profile.ID + "\x00" + hex.EncodeToString(digest[:])
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	current := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, method, current); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(current)) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start, end, next, err := s.mutations.pageRange(peer, scope, request.GetPage(), cfg, len(logs))
	if err != nil {
		return nil, err
	}
	return &ipc.ListRecentLogsResponse{Logs: logs[start:end], Page: &ipc.PageResponse{
		NextPageToken: next,
		Metadata:      &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())},
	}}, nil
}
