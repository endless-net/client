// Package localipc implements the authenticated local Client boundary. It does
// not accept caller identities or administrator flags from RPC metadata.
package localipc

import (
	"context"
	"encoding/hex"
	"strings"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Principal struct {
	SID           string
	Administrator bool
}

// AuthInfo can only be created on a real connection by the pipe credentials.
type AuthInfo struct {
	credentials.CommonAuthInfo
	principal Principal
}

func (AuthInfo) AuthType() string { return "windows-named-pipe" }

func Caller(ctx context.Context) (Principal, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Principal{}, status.Error(codes.Unauthenticated, "local OS caller required")
	}
	info, ok := p.AuthInfo.(AuthInfo)
	if !ok || info.principal.SID == "" {
		return Principal{}, status.Error(codes.Unauthenticated, "local OS caller required")
	}
	return info.principal, nil
}

// OwnerReader reads the durable installation owner. Empty means unclaimed,
// not a wildcard. Handlers with an initial-claim annotation must atomically
// claim ownership with the first persisted mutation; this interceptor cannot
// replace that transaction.
type OwnerReader func(context.Context) (string, error)

// Observation surrounds authorization without replacing it. The application
// supplies both interceptors; test fixtures explicitly supply their own sinks.
type Observation struct {
	Unary  grpc.UnaryServerInterceptor
	Stream grpc.StreamServerInterceptor
}

func Authorize(ctx context.Context, method string, readOwner OwnerReader) error {
	caller, err := Caller(ctx)
	if err != nil {
		return err
	}
	const prefix = "/client.v0.ClientService/"
	if !strings.HasPrefix(method, prefix) {
		return status.Error(codes.PermissionDenied, "unknown local operation")
	}
	service := clientv0.File_client_v0_service_proto.Services().ByName("ClientService")
	descriptor := service.Methods().ByName(protoreflect.Name(strings.TrimPrefix(method, prefix)))
	if descriptor == nil || !proto.HasExtension(descriptor.Options(), clientv0.E_Access) {
		return status.Error(codes.PermissionDenied, "unknown local operation")
	}
	access := proto.GetExtension(descriptor.Options(), clientv0.E_Access).(clientv0.Access)
	switch access {
	case clientv0.Access_ACCESS_OBSERVER:
		return nil
	case clientv0.Access_ACCESS_ADMINISTRATOR:
		if caller.Administrator {
			return nil
		}
	case clientv0.Access_ACCESS_OWNER:
		if readOwner == nil {
			return status.Error(codes.Unavailable, "owner store unavailable")
		}
		owner, err := readOwner(ctx)
		if err != nil {
			return status.Error(codes.Unavailable, "owner store unavailable")
		}
		if owner == caller.SID {
			return nil
		}
		initial := proto.GetExtension(descriptor.Options(), clientv0.E_AllowsInitialOwnershipClaim).(bool)
		if owner == "" && initial {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "local operation not authorized")
}

func Unary(readOwner OwnerReader) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := Authorize(ctx, info.FullMethod, readOwner); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func Stream(readOwner OwnerReader) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := Authorize(stream.Context(), info.FullMethod, readOwner); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

// CheckRuntimeInfo uses the already published bootstrap fields. IPC version 0 is
// an intentional version, not an unspecified value. A mismatched pair fails
// before the caller exposes mutation methods or attempts enrollment.
func CheckRuntimeInfo(info *clientv0.RuntimeInfo, instance string, digest [32]byte) error {
	if info == nil || instance == "" || digest == [32]byte{} || info.InstanceId != instance ||
		info.Protocol != "endlessnet-client-ipc" || info.IpcVersion != 0 ||
		info.ContractSha256 != hex.EncodeToString(digest[:]) {
		return status.Error(codes.FailedPrecondition, "local Client contract mismatch")
	}
	return nil
}
