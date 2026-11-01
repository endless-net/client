//go:build windows && integration

package localipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type authenticatedService struct {
	clientv0.UnimplementedClientServiceServer
	caller chan Principal
	config PipeConfig
}

func (s *authenticatedService) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	p, err := Caller(ctx)
	if err != nil {
		return nil, err
	}
	s.caller <- p
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{InstanceId: s.config.InstanceID, Protocol: "endlessnet-client-ipc", IpcVersion: 0, ContractSha256: hex.EncodeToString(s.config.ContractDigest[:])}}, nil
}

func TestNamedPipeGRPCAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("real Windows IPC acceptance is separate from unit CI")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	config := PipeConfig{InstanceID: fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:]), ServerSID: sid, ContractDigest: [32]byte{7}, HandshakeTimeout: 2 * time.Second}
	listener, err := Listen(config, []string{sid})
	if err != nil {
		t.Fatal(err)
	}
	implementation := &authenticatedService{caller: make(chan Principal, 4), config: config}
	server, err := NewServer(config, func(context.Context) (string, error) { return "another-OS-owner", nil }, implementation, 65536, 16, testServerObservation())
	if err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("gRPC pipe server did not stop")
		}
	})
	creds, err := Credentials(config)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient("passthrough:///endlessnet-local", grpc.WithTransportCredentials(creds), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return Dial(ctx, config) }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := clientv0.NewClientServiceClient(conn)
	if _, err := client.GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case caller := <-implementation.caller:
		if caller.SID != sid {
			t.Fatal("OS identity lost across HTTP2/gRPC")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	badConfig := config
	badConfig.ContractDigest[0]++
	if channel, err := Connect(ctx, badConfig); status.Code(err) != codes.FailedPrecondition || channel != nil {
		t.Fatalf("mismatched bootstrap digest accepted: %v", err)
	}
	paired, err := Connect(ctx, config)
	if err != nil {
		t.Fatalf("published GetRuntimeInfo bootstrap failed: %v", err)
	}
	if err := paired.Close(); err != nil {
		t.Fatal(err)
	}
	forged := metadata.NewOutgoingContext(ctx, metadata.Pairs("sid", "another-OS-owner", "administrator", "true"))
	if _, err := client.GetEnrollmentAttempt(forged, &clientv0.GetEnrollmentAttemptRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign owner/metadata bypassed authorization: %v", err)
	}
}

// Explicit test sink: authorization remains inside NewServer and production
// application composition supplies its real Signals interceptors.
func testServerObservation() Observation {
	return Observation{
		Unary: func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
			return next(ctx, request)
		},
		Stream: func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
			return next(server, stream)
		},
	}
}
