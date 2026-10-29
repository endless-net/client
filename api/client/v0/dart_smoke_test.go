// The handlers below are isolated conformance fixtures, not a production runtime or an OS authorization adapter.
package clientv0_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/api/client/v0/clientv0connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

type dartSmokeServer struct {
	clientv0connect.UnimplementedClientServiceHandler
	digest            string
	cancelled         chan struct{}
	cancellationAck   atomic.Bool
	forwardedDeadline atomic.Bool
	deadline          atomic.Bool
}

func (s *dartSmokeServer) GetRuntimeInfo(ctx context.Context, request *connect.Request[clientv0.GetRuntimeInfoRequest]) (*connect.Response[clientv0.GetRuntimeInfoResponse], error) {
	// This metadata is private to the test fixture, not part of the IPC contract.
	if request.Header().Get("X-Endlessnet-Conformance-Probe") == "watch-cancelled" {
		ackContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		select {
		case <-s.cancelled:
			s.cancellationAck.Store(true)
		case <-ackContext.Done():
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("CONFORMANCE_CANCEL_NOT_OBSERVED"))
		}
	}
	return connect.NewResponse(&clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{InstanceId: "fixture-instance", ContractSha256: s.digest, Protocol: "endlessnet-client-ipc", IpcVersion: 0}}), nil
}
func (s *dartSmokeServer) GetStatus(_ context.Context, request *connect.Request[clientv0.GetStatusRequest]) (*connect.Response[clientv0.GetStatusResponse], error) {
	if got := request.Header().Values("X-Endlessnet-Ipc-Contract-Sha256"); len(got) != 1 || got[0] != s.digest {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("fixture digest mismatch"))
	}
	return connect.NewResponse(&clientv0.GetStatusResponse{Status: &clientv0.Status{Metadata: &clientv0.SnapshotMetadata{InstanceId: "fixture-instance", Revision: 7}, ConnectionPhase: clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED}}), nil
}
func (*dartSmokeServer) Disconnect(context.Context, *connect.Request[clientv0.DisconnectRequest]) (*connect.Response[clientv0.DisconnectResponse], error) {
	failure := connect.NewError(connect.CodePermissionDenied, fmt.Errorf("fixture denial"))
	detail, err := connect.NewErrorDetail(&clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_OWNER_REQUIRED, ReasonKey: "fixture.owner_required"})
	if err != nil {
		return nil, err
	}
	failure.AddDetail(detail)
	return nil, failure
}
func (s *dartSmokeServer) WatchEvents(ctx context.Context, _ *connect.Request[clientv0.WatchEventsRequest], stream *connect.ServerStream[clientv0.WatchEventsResponse]) error {
	if err := stream.Send(&clientv0.WatchEventsResponse{Sequence: 1, Event: &clientv0.WatchEventsResponse_Snapshot{Snapshot: &clientv0.SnapshotEvent{Runtime: &clientv0.RuntimeInfo{InstanceId: "fixture-instance", ContractSha256: s.digest, Protocol: "endlessnet-client-ipc"}, Status: &clientv0.Status{ConnectionPhase: clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED}}}}); err != nil {
		return err
	}
	if err := stream.Send(&clientv0.WatchEventsResponse{Sequence: 2, Event: &clientv0.WatchEventsResponse_Invalidated{Invalidated: &clientv0.DomainInvalidated{Domain: clientv0.Domain_DOMAIN_PEERS, ProfileId: "profile-fixture"}}}); err != nil {
		return err
	}
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.Canceled) {
		close(s.cancelled)
	}
	return connect.NewError(connect.CodeCanceled, ctx.Err())
}
func (s *dartSmokeServer) GetEnrollmentAttempt(ctx context.Context, _ *connect.Request[clientv0.GetEnrollmentAttemptRequest]) (*connect.Response[clientv0.GetEnrollmentAttemptResponse], error) {
	inboundDeadline, ok := ctx.Deadline()
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("CONFORMANCE_MISSING_DEADLINE"))
	}
	const serverWait = 100 * time.Millisecond
	if time.Until(inboundDeadline) <= 2*serverWait {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("CONFORMANCE_INBOUND_DEADLINE_TOO_SHORT"))
	}
	s.forwardedDeadline.Store(true)
	// Expire a real server context before the longer client timer. A local Dart
	// timeout must not stand in for a received server DeadlineExceeded outcome.
	serverContext, cancel := context.WithTimeout(ctx, serverWait)
	defer cancel()
	<-serverContext.Done()
	if !errors.Is(serverContext.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("CONFORMANCE_SERVER_DEADLINE_NOT_EXPIRED"))
	}
	s.deadline.Store(true)
	return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("CONFORMANCE_SERVER_DEADLINE_EXCEEDED"))
}

func TestGoDartLocalGRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("transport interop is outside the MVP unit-only suite")
	}
	if os.Getenv("ENDLESSNET_DART_SMOKE") != "1" {
		t.Skip("set ENDLESSNET_DART_SMOKE=1 after dart pub get; no transport pass is claimed when skipped")
	}
	mutation := os.Getenv("ENDLESSNET_CONFORMANCE_MUTATION")
	if mutation != "" && mutation != "drop-timeout" && mutation != "omit-cancel" {
		t.Fatalf("CONFORMANCE_UNKNOWN_MUTATION=%q", mutation)
	}
	descriptor, err := os.ReadFile(filepath.Join("..", "..", "client.binpb"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	digest := fmt.Sprintf("%x", sha256.Sum256(descriptor))
	network, address := "unix", filepath.Join(t.TempDir(), "ipc.sock")
	if runtime.GOOS == "windows" {
		network, address = "tcp", "127.0.0.1:0"
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, network, address)
	if err != nil {
		t.Fatal(err)
	}
	if network == "tcp" {
		address = listener.Addr().String()
	}
	fixture := &dartSmokeServer{digest: digest, cancelled: make(chan struct{})}
	mux := http.NewServeMux()
	path, handler := clientv0connect.NewClientServiceHandler(fixture)
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if mutation == "drop-timeout" && request.URL.Path == clientv0connect.ClientServiceGetEnrollmentAttemptProcedure {
			request.Header.Del("Grpc-Timeout")
		}
		handler.ServeHTTP(w, request)
	}))
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Handler: mux, Protocols: protocols, ReadHeaderTimeout: time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-serveDone; err != nil && err != http.ErrServerClosed {
			t.Logf("test server: %v", err)
		}
	})
	// The generated grpc-Go client and Dart client use the same gRPC wire handler.
	connection, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	runtimeInfo, err := clientv0.NewClientServiceClient(connection).GetRuntimeInfo(ctx, &clientv0.GetRuntimeInfoRequest{})
	if err != nil || runtimeInfo.GetRuntime().GetContractSha256() != digest {
		t.Fatalf("generated grpc-Go client: %v %v", runtimeInfo, err)
	}
	dart := os.Getenv("ENDLESSNET_DART_BINARY")
	if dart == "" {
		dart = "dart"
	}
	command := exec.CommandContext(ctx, dart, "run", "test/transport_smoke.dart", network, address, digest, mutation)
	command.Dir = filepath.Join("..", "..", "dart")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Dart smoke: %v\n%s", err, output)
	}
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "DART_VECTOR=") {
			wire, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(line, "DART_VECTOR=")))
			if err != nil {
				t.Fatal(err)
			}
			var fromDart clientv0.SelectExitNodeRequest
			if err := proto.Unmarshal(wire, &fromDart); err != nil || fromDart.GetMutation().GetExpectedRevision() != 7 || fromDart.GetExitNodeId() != "exit-fixture" {
				t.Fatalf("Dart vector decode: %v %v", &fromDart, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("Dart omitted serialization vector: %s", output)
	}
	if !fixture.cancellationAck.Load() || !fixture.forwardedDeadline.Load() || !fixture.deadline.Load() {
		t.Fatalf("CONFORMANCE_SERVER_OBSERVATION_MISSING: %s", output)
	}
	t.Logf("CONFORMANCE_PASS deadline_forwarded=true server_expired=true cancellation_ack=true; %s %s\n%s", network, runtime.GOOS, output)
}
