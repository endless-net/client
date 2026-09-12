// Package rpc enforces the Client v0 contract at generated RPC boundaries.
package rpc

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	clientipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	Protocol         = "endlessnet-client-ipc"
	Version          = 0
	ProtocolHeader   = "X-EndlessNet-IPC-Protocol"
	VersionHeader    = "X-EndlessNet-IPC-Version"
	DigestHeader     = "X-EndlessNet-IPC-Contract-SHA256"
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 4 << 20
)

//go:embed client.binpb
var descriptor []byte

// Digest identifies the descriptor built by the pinned Buf generation pipeline.
func Digest() string {
	sum := sha256.Sum256(descriptor)
	return hex.EncodeToString(sum[:])
}

func Error(code connect.Code, failure clientipc.ErrorCode) error {
	err := connect.NewError(code, errors.New(failure.String()))
	detail, detailErr := connect.NewErrorDetail(&clientipc.Failure{
		Code:      failure,
		ReasonKey: strings.ToLower(failure.String()),
	})
	if detailErr == nil {
		err.AddDetail(detail)
	}
	return err
}

// FailureFromError returns only typed contract details, never a diagnostic string.
func FailureFromError(err error) *clientipc.Failure {
	var rpcErr *connect.Error
	if !errors.As(err, &rpcErr) {
		return nil
	}
	for _, detail := range rpcErr.Details() {
		value, decodeErr := detail.Value()
		if decodeErr != nil {
			continue
		}
		if failure, ok := value.(*clientipc.Failure); ok {
			return failure
		}
	}
	return nil
}

// RequiredAccess derives authorization from the canonical method annotation.
func RequiredAccess(procedure string) (clientipc.Access, bool) {
	const prefix = "/client.v0.ClientService/"
	if !strings.HasPrefix(procedure, prefix) {
		return clientipc.Access_ACCESS_UNSPECIFIED, false
	}
	name := strings.TrimPrefix(procedure, prefix)
	service := clientipc.File_client_v0_service_proto.Services().ByName("ClientService")
	method := service.Methods().ByName(protoreflect.Name(name))
	if method == nil || !proto.HasExtension(method.Options(), clientipc.E_Access) {
		return clientipc.Access_ACCESS_UNSPECIFIED, false
	}
	access, ok := proto.GetExtension(method.Options(), clientipc.E_Access).(clientipc.Access)
	return access, ok && access != clientipc.Access_ACCESS_UNSPECIFIED
}

// Guard must be installed on every generated handler. Authorize derives identity
// from the local transport context; it must not trust any request metadata.
type Guard struct {
	Authorize func(context.Context, clientipc.Access, string) error
}

func (g Guard) check(ctx context.Context, procedure string, header http.Header) error {
	access, exists := RequiredAccess(procedure)
	if !exists {
		return Error(connect.CodeUnimplemented, clientipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if g.Authorize == nil {
		return Error(connect.CodeUnauthenticated, clientipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}
	if err := g.Authorize(ctx, access, procedure); err != nil {
		return err
	}
	if procedure == clientipcconnect.ClientServiceGetRuntimeInfoProcedure {
		return nil // An authenticated caller may diagnose mismatched installation.
	}
	for key, want := range map[string]string{
		ProtocolHeader: Protocol, VersionHeader: "0", DigestHeader: Digest(),
	} {
		values := header.Values(key)
		if len(values) != 1 || values[0] != want {
			return Error(connect.CodeFailedPrecondition, clientipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH)
		}
	}
	return nil
}

func (g Guard) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := g.check(ctx, req.Spec().Procedure, req.Header()); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (g Guard) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := g.check(ctx, conn.Spec().Procedure, conn.RequestHeader()); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (Guard) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// ClientHeaders is shared by CLI/helper and other generated Go consumers.
type ClientHeaders struct{}

func SetHeaders(header http.Header) {
	header.Set(ProtocolHeader, Protocol)
	header.Set(VersionHeader, "0")
	header.Set(DigestHeader, Digest())
}

func (ClientHeaders) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		SetHeaders(req.Header())
		return next(ctx, req)
	}
}

func (ClientHeaders) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		SetHeaders(conn.RequestHeader())
		return conn
	}
}

func (ClientHeaders) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
