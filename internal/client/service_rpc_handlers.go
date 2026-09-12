package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"google.golang.org/protobuf/proto"
)

// ClientRPCService is the native v0 runtime implementation under construction.
// Production listener cutover happens after its remaining domain methods and
// consumers are migrated. It never forwards requests to the HTTP v2 handler.
type ClientRPCService struct {
	clientipcconnect.UnimplementedClientServiceHandler
	mutations      *ClientRPCMutations
	build          *ipc.BuildIdentity
	profileMu      sync.Mutex
	profileWorker  *clientRPCProfileWorker
	disconnectGate chan struct{}
}

func NewClientRPCService(mutations *ClientRPCMutations, build *ipc.BuildIdentity) *ClientRPCService {
	if build == nil {
		build = &ipc.BuildIdentity{}
	}
	return &ClientRPCService{mutations: mutations, build: proto.Clone(build).(*ipc.BuildIdentity), disconnectGate: make(chan struct{}, 1)}
}

func (s *ClientRPCService) Handler() http.Handler {
	_, handler := clientipcconnect.NewClientServiceHandler(s,
		connect.WithInterceptors(rpc.Guard{Authorize: s.mutations.Authorize}, runtimeRPCFailureInterceptor{}),
		connect.WithReadMaxBytes(rpc.MaxRequestBytes), connect.WithSendMaxBytes(rpc.MaxResponseBytes))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		abort := func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Now()); cancel() }
		ctx = context.WithValue(ctx, rpcStreamAbortKey{}, abort)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

type rpcStreamAbortKey struct{}

// Never serialize filesystem/provider diagnostics at the local RPC boundary.
type runtimeRPCFailureInterceptor struct{}

func runtimeRPCFailure(err error) error {
	if err == nil || rpc.FailureFromError(err) != nil {
		return err
	}
	code := connect.CodeOf(err)
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	failure, ok := map[connect.Code]ipc.ErrorCode{
		connect.CodeCanceled:          ipc.ErrorCode_ERROR_CODE_CANCELLED,
		connect.CodeDeadlineExceeded:  ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED,
		connect.CodeUnimplemented:     ipc.ErrorCode_ERROR_CODE_UNSUPPORTED,
		connect.CodeUnavailable:       ipc.ErrorCode_ERROR_CODE_UNAVAILABLE,
		connect.CodeInvalidArgument:   ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
		connect.CodeResourceExhausted: ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED,
		connect.CodeNotFound:          ipc.ErrorCode_ERROR_CODE_NOT_FOUND,
	}[code]
	if !ok {
		code, failure = connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL
	}
	return rpc.Error(code, failure)
}

func (runtimeRPCFailureInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		response, err := next(ctx, request)
		return response, runtimeRPCFailure(err)
	}
}

func (runtimeRPCFailureInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return runtimeRPCFailure(next(ctx, conn))
	}
}

func (runtimeRPCFailureInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (s *ClientRPCService) GetRuntimeInfo(ctx context.Context, _ *connect.Request[ipc.GetRuntimeInfoRequest]) (*connect.Response[ipc.GetRuntimeInfoResponse], error) {
	peer, ok := local.PeerFromContext(ctx)
	if !ok {
		return nil, rpc.Error(connect.CodeUnauthenticated, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}
	access := ipc.Access_ACCESS_OBSERVER
	if peer.Administrator {
		access = ipc.Access_ACCESS_ADMINISTRATOR
	} else if cfg := s.mutations.store.Read(); cfg.LocalOwnerID != "" && strings.EqualFold(cfg.LocalOwnerID, peer.Identity) {
		access = ipc.Access_ACCESS_OWNER
	}
	return connect.NewResponse(&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{
		Build: proto.Clone(s.build).(*ipc.BuildIdentity), InstanceId: s.mutations.instanceID,
		CallerAccess: access, Protocol: rpc.Protocol, IpcVersion: rpc.Version, ContractSha256: rpc.Digest(),
		// Capabilities remain absent until the complete provider family is ready.
	}}), nil
}

func (s *ClientRPCService) GetOperation(ctx context.Context, request *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
	op, err := s.mutations.GetOperation(ctx, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.GetOperationResponse{Operation: op}), nil
}

func (s *ClientRPCService) GetStatus(ctx context.Context, _ *connect.Request[ipc.GetStatusRequest]) (*connect.Response[ipc.GetStatusResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	snapshot, err := s.mutations.snapshotAs(peer, s.build)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.GetStatusResponse{Status: snapshot.Status}), nil
}

func (s *ClientRPCService) WatchEvents(ctx context.Context, _ *connect.Request[ipc.WatchEventsRequest], stream *connect.ServerStream[ipc.WatchEventsResponse]) error {
	peer, _ := local.PeerFromContext(ctx)
	abort, _ := ctx.Value(rpcStreamAbortKey{}).(func())
	subscriber, err := s.mutations.subscribe(peer, s.build, abort)
	if err != nil {
		return err
	}
	defer s.mutations.unsubscribe(subscriber)
	for {
		event, err := subscriber.next(ctx)
		if err != nil {
			return err
		}
		if err := subscriber.send(stream, event); err != nil {
			return err
		}
	}
}

func (s *ClientRPCService) CreateProfile(ctx context.Context, request *connect.Request[ipc.CreateProfileRequest]) (*connect.Response[ipc.CreateProfileResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.mutations.createProfileAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.CreateProfileResponse{Operation: op}), nil
}

func (s *ClientRPCService) ListProfiles(ctx context.Context, request *connect.Request[ipc.ListProfilesRequest]) (*connect.Response[ipc.ListProfilesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	ready := s.profileWorker != nil && s.profileWorker.ctx.Err() == nil
	result, err := s.mutations.listProfilesWithSelectionAs(peer, request.Msg, ready)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) RenameProfile(ctx context.Context, request *connect.Request[ipc.RenameProfileRequest]) (*connect.Response[ipc.RenameProfileResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.mutations.renameProfileAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.RenameProfileResponse{Operation: op}), nil
}

func (s *ClientRPCService) RemoveProfile(ctx context.Context, request *connect.Request[ipc.RemoveProfileRequest]) (*connect.Response[ipc.RemoveProfileResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.mutations.removeProfileAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.RemoveProfileResponse{Operation: op}), nil
}
