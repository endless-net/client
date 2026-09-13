//go:build windows || linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	"github.com/endless-net/client/clientipc/testserver"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNativeBundleExportLocalTransport(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-profile", "pending", "chunk-failure", "digest"} {
		t.Run(mode, func(t *testing.T) {
			endpoint := fmt.Sprintf(`\\.\pipe\en-bundle-cli-%d`, time.Now().UnixNano())
			transportFlag := "--ipc-pipe"
			if runtime.GOOS != "windows" {
				dir, err := os.MkdirTemp("/tmp", "en-bdl-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Remove(dir); err != nil {
						t.Error(err)
					}
				})
				endpoint, transportFlag = filepath.Join(dir, "rpc.sock"), "--ipc-socket"
			}
			fixture := testserver.New()
			expect := func(step testserver.Step) {
				t.Helper()
				if err := fixture.Expect(step); err != nil {
					t.Fatal(err)
				}
			}
			expect(testserver.Step{Method: "GetRuntimeInfo", Request: &ipc.GetRuntimeInfoRequest{}, Responses: []proto.Message{&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{Protocol: rpc.Protocol, ContractSha256: rpc.Digest(), InstanceId: "runtime"}}}})
			payload := []byte("verified archive")
			digest := sha256.Sum256(payload)
			now := time.Now()
			bundle := &ipc.BundleResult{BundleId: "opaque-handle", CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(15 * time.Minute)), SizeBytes: uint64(len(payload)), Sha256: hex.EncodeToString(digest[:])}
			op := &ipc.Operation{Id: "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", ProfileId: "profile", Kind: ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE, State: ipc.OperationState_OPERATION_STATE_SUCCEEDED, Outcome: &ipc.Operation_Bundle{Bundle: bundle}}
			switch mode {
			case "wrong-profile":
				op.ProfileId = "other"
			case "pending":
				op.State = ipc.OperationState_OPERATION_STATE_PENDING
			case "digest":
				bundle.Sha256 = hex.EncodeToString(make([]byte, 32))
			}
			expect(testserver.Step{Method: "GetOperation", Request: &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}}, Responses: []proto.Message{&ipc.GetOperationResponse{Operation: op}}})
			if mode != "wrong-profile" && mode != "pending" {
				expect(testserver.Step{Method: "ReadDiagnosticsBundle", Request: &ipc.ReadDiagnosticsBundleRequest{BundleId: bundle.BundleId, MaxBytes: 64 << 10}, Responses: []proto.Message{&ipc.ReadDiagnosticsBundleResponse{Data: payload[:4], NextOffset: 4}}})
				step := testserver.Step{Method: "ReadDiagnosticsBundle", Request: &ipc.ReadDiagnosticsBundleRequest{BundleId: bundle.BundleId, Offset: 4, MaxBytes: 64 << 10}}
				if mode == "chunk-failure" {
					step.Err = rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
				} else {
					step.Responses = []proto.Message{&ipc.ReadDiagnosticsBundleResponse{Data: payload[4:], NextOffset: uint64(len(payload)), Eof: true}}
				}
				expect(step)
			}
			listener, err := local.Listen(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			server := local.NewServer(fixture.Handler(func(ctx context.Context, _ ipc.Access, _ string) error {
				if _, ok := local.PeerFromContext(ctx); !ok {
					return fmt.Errorf("missing local peer")
				}
				return nil
			}))
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			defer func() {
				_ = server.Close()
				<-done
				if err := fixture.Verify(); err != nil {
					t.Error(err)
				}
			}()
			output, err := captureStdout(t, func() error {
				return cmdService([]string{"export-diagnostics-bundle", transportFlag, endpoint, "--operation-id", op.Id, "--profile-id", "profile", "--timeout", "5s"})
			})
			if mode == "valid" {
				if err != nil || output != string(payload) {
					t.Fatal("native export corrupted archive", err)
				}
			} else if err == nil || output != "" {
				t.Fatal("failed export emitted partial archive")
			}
			if mode == "chunk-failure" && rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED {
				t.Fatal("export lost typed revocation failure")
			}
		})
	}
}
