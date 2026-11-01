//go:build windows && integration

package localipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type dartPipeFixture struct {
	clientv0.UnimplementedClientServiceServer
	config   PipeConfig
	owner    string
	runtimes atomic.Int32
	statuses atomic.Int32
	enrolls  atomic.Int32
	slow     chan struct{}
}

func (f *dartPipeFixture) GetRuntimeInfo(ctx context.Context, _ *clientv0.GetRuntimeInfoRequest) (*clientv0.GetRuntimeInfoResponse, error) {
	caller, err := Caller(ctx)
	if err != nil || caller.SID != f.owner {
		return nil, status.Error(codes.Unauthenticated, "actual OS caller required")
	}
	f.runtimes.Add(1)
	return &clientv0.GetRuntimeInfoResponse{Runtime: &clientv0.RuntimeInfo{InstanceId: f.config.InstanceID, Protocol: "endlessnet-client-ipc", IpcVersion: 0, ContractSha256: hex.EncodeToString(f.config.ContractDigest[:])}}, nil
}
func (f *dartPipeFixture) GetStatus(context.Context, *clientv0.GetStatusRequest) (*clientv0.GetStatusResponse, error) {
	f.statuses.Add(1)
	return &clientv0.GetStatusResponse{Status: &clientv0.Status{Metadata: &clientv0.SnapshotMetadata{InstanceId: f.config.InstanceID, Revision: ^uint64(0)}, Network: &clientv0.Network{Name: strings.Repeat("x", 131072)}}}, nil
}
func (f *dartPipeFixture) Enroll(context.Context, *clientv0.EnrollRequest) (*clientv0.EnrollResponse, error) {
	f.enrolls.Add(1)
	st, err := status.New(codes.PermissionDenied, "fixture-raw-diagnostic").WithDetails(&clientv0.Failure{Code: clientv0.ErrorCode_ERROR_CODE_OWNER_REQUIRED, ReasonKey: "client.owner.required", ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER})
	if err != nil {
		return nil, err
	}
	return nil, st.Err()
}
func (f *dartPipeFixture) GetEnrollmentAttempt(ctx context.Context, _ *clientv0.GetEnrollmentAttemptRequest) (*clientv0.GetEnrollmentAttemptResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.Internal, "wire deadline missing")
	}
	<-ctx.Done()
	close(f.slow)
	return nil, status.FromContextError(ctx.Err()).Err()
}

func TestWindowsDartUIOwnedPipe(t *testing.T) {
	if testing.Short() || os.Getenv("ENDLESSNET_UI_PIPE_PROBE") == "" {
		t.Skip("explicit native Dart UI/Go authenticated pipe fixture")
	}
	dart := os.Getenv("ENDLESSNET_DART_BINARY")
	probe := os.Getenv("ENDLESSNET_UI_PIPE_PROBE")
	library := os.Getenv("ENDLESSNET_UI_PIPE_LIBRARY")
	if !filepath.IsAbs(dart) || !filepath.IsAbs(probe) || !filepath.IsAbs(library) {
		t.Fatal("explicit absolute Dart binary/probe paths required")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	config := PipeConfig{InstanceID: id, ServerSID: sid, ContractDigest: [32]byte{1}, HandshakeTimeout: 3 * time.Second}
	listener, err := Listen(config, []string{sid})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &dartPipeFixture{config: config, owner: sid, slow: make(chan struct{})}
	server, err := NewServer(config, func(context.Context) (string, error) { return sid, nil }, fixture, 1<<20, 16, testServerObservation())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("pipe fixture did not stop")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dart, "run", probe, id, sid, hex.EncodeToString(config.ContractDigest[:]), library)
	cmd.Dir = filepath.Dir(filepath.Dir(filepath.Dir(probe))) // UI app package.
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("fixture calls: runtime=%d status=%d enroll=%d", fixture.runtimes.Load(), fixture.statuses.Load(), fixture.enrolls.Load())
		t.Fatalf("Dart pipe fixture failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "WINDOWS_DART_GO_PIPE_OWNER_DIGEST_FAILURE_DEADLINE_PASS") {
		t.Fatal("Dart pipe fixture success marker missing")
	}
	select {
	case <-fixture.slow:
	case <-ctx.Done():
		t.Fatal("Dart deadline did not cancel real server context")
	}
	if fixture.runtimes.Load() != 3 || fixture.statuses.Load() != 1 || fixture.enrolls.Load() != 1 {
		t.Fatal("wrong owner reached RPC, automatic retry or unexpected fixture calls")
	}
}
