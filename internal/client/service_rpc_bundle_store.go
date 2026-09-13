package client

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const rpcBundleStoreMaxBytes = 4 * rpcBundleMaxBytes

type clientRPCBundleRecord struct {
	owner, profile string
	data           []byte
	metadata       *ipc.BundleResult
}

// Runtime integration must revoke on owner/profile/logout changes. The zero
// value is memory-only; openClientRPCBundleStore supplies durable persistence.
type clientRPCBundleStore struct {
	mu      sync.Mutex
	items   map[string]clientRPCBundleRecord
	bytes   int
	now     func() time.Time
	persist func(map[string]clientRPCBundleRecord) error
	dirty   bool
}

func (s *clientRPCBundleStore) pruneLocked() {
	now := s.timeNow()
	for id, item := range s.items {
		if !now.Before(item.metadata.ExpiresAt.AsTime()) {
			delete(s.items, id)
			s.dirty = true
			s.bytes -= len(item.data)
		}
	}
}

func (s *clientRPCBundleStore) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *clientRPCBundleStore) put(owner, profile string, data []byte) (*ipc.BundleResult, error) {
	id, err := newRPCUUID()
	if err != nil {
		return nil, err
	}
	return s.putID(id, owner, profile, data)
}

func (s *clientRPCBundleStore) putID(id, owner, profile string, data []byte) (*ipc.BundleResult, error) {
	if !validRPCUUID(id) {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if owner == "" || profile == "" || len(owner) > 4096 || len(profile) > 4096 || len(data) == 0 || len(data) > rpcBundleMaxBytes {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if _, exists := s.items[id]; exists {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if len(s.items) >= 32 || s.bytes+len(data) > rpcBundleStoreMaxBytes {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	now := s.timeNow()
	digest := sha256.Sum256(data)
	metadata := &ipc.BundleResult{BundleId: id, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(15 * time.Minute)), SizeBytes: uint64(len(data)), Sha256: hex.EncodeToString(digest[:])}
	if metadata.CreatedAt.CheckValid() != nil || metadata.ExpiresAt.CheckValid() != nil {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	if s.items == nil {
		s.items = map[string]clientRPCBundleRecord{}
	}
	s.items[id] = clientRPCBundleRecord{owner: owner, profile: profile, data: append([]byte(nil), data...), metadata: metadata}
	if s.persist != nil {
		if err := s.persist(s.items); err != nil {
			delete(s.items, id)
			return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
	}
	s.bytes += len(data)
	s.dirty = false
	return proto.Clone(metadata).(*ipc.BundleResult), nil
}

func (s *clientRPCBundleStore) read(owner, profile string, request *ipc.ReadDiagnosticsBundleRequest) (*ipc.ReadDiagnosticsBundleResponse, error) {
	if request == nil || !validRPCUUID(request.BundleId) || request.MaxBytes > 256<<10 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	item, ok := s.items[request.BundleId]
	if !ok || !strings.EqualFold(item.owner, owner) || item.profile != profile {
		return nil, rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	if request.Offset > uint64(len(item.data)) {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	size := request.MaxBytes
	if size == 0 {
		size = 64 << 10
	}
	end := min(request.Offset+uint64(size), uint64(len(item.data)))
	return &ipc.ReadDiagnosticsBundleResponse{Data: append([]byte(nil), item.data[request.Offset:end]...), NextOffset: end, Eof: end == uint64(len(item.data))}, nil
}

func (s *clientRPCBundleStore) revoke(owner, profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := make(map[string]clientRPCBundleRecord, len(s.items))
	remainingBytes := 0
	for id, item := range s.items {
		if !strings.EqualFold(item.owner, owner) || item.profile != profile {
			remaining[id] = item
			remainingBytes += len(item.data)
		}
	}
	if s.persist != nil {
		if err := s.persist(remaining); err != nil {
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
	}
	s.items, s.bytes = remaining, remainingBytes
	s.dirty = false
	return nil
}
