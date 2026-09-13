//go:build windows || linux || darwin

package client

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCBundleNativeTransportLifecycle(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	var err error
	s.bundleStore, err = openClientRPCBundleStore(m.store.path+".bundles", m.now)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.DiagnosticsProvider = func(ctx context.Context) (ClientRPCDiagnosticsObservation, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ClientRPCDiagnosticsObservation{}, ctx.Err()
		}
		return ClientRPCDiagnosticsObservation{OSVersion: "test-os", Tunnel: WireGuardInspection{Error: "private_key=synthetic-private-value"}}, nil
	}
	workerCtx, stopWorker := context.WithCancel(t.Context())
	workerDone, err := s.startBundleWorker(workerCtx)
	if err != nil {
		stopWorker()
		t.Fatal(err)
	}
	defer func() {
		stopWorker()
		select {
		case <-workerDone:
		case <-time.After(3 * time.Second):
			t.Error("bundle worker did not stop")
		}
	}()
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-bundle-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-bundle-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := local.NewServer(s.Handler())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-serverDone; err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	info, err := consumer.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(&ipc.ReadDiagnosticsBundleRequest{BundleId: "00000000-0000-4000-8000-000000000000"}))
	assertRPCFailure(t, err, rpcUnownedMissingResourceFailure(t, info))
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://control.example.test"
	created, err := consumer.CreateProfile(ctx, connect.NewRequest(create))
	if err != nil {
		t.Fatal(err)
	}
	profile := &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}
	if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ActiveProfileID = profile.ProfileId; return nil }); err != nil {
		t.Fatal(err)
	}
	request := &ipc.CreateDiagnosticsBundleRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	accepted, err := consumer.CreateDiagnosticsBundle(requestCtx, connect.NewRequest(request))
	cancelRequest()
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Msg.Operation.State != ipc.OperationState_OPERATION_STATE_PENDING {
		t.Fatal("admission reported completion")
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("worker did not collect")
	}
	_, err = consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(&ipc.ReadDiagnosticsBundleRequest{BundleId: accepted.Msg.Operation.Id}))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	close(release)
	var completed *ipc.Operation
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for completed == nil {
		response, err := consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: accepted.Msg.Operation.Id}}))
		if err != nil {
			t.Fatal(err)
		}
		if rpcOperationTerminal(response.Msg.Operation.State) {
			completed = response.Msg.Operation
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("operation did not finish")
		}
	}
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.GetBundle() == nil {
		t.Fatal("bundle failed", completed)
	}
	metadata := completed.GetBundle()
	var data []byte
	for {
		response, err := consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(&ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId, Offset: uint64(len(data)), MaxBytes: 64}))
		if err != nil {
			t.Fatal(err)
		}
		chunk := response.Msg
		if len(chunk.Data) == 0 || len(chunk.Data) > 64 || chunk.NextOffset != uint64(len(data)+len(chunk.Data)) {
			t.Fatal("invalid chunk")
		}
		data = append(data, chunk.Data...)
		if chunk.Eof {
			break
		}
	}
	digest := sha256.Sum256(data)
	if uint64(len(data)) != metadata.SizeBytes || hex.EncodeToString(digest[:]) != metadata.Sha256 {
		t.Fatal("archive descriptor mismatch")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != "diagnostics.json" {
		t.Fatal("invalid archive", err)
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || strings.Contains(string(raw), "synthetic-private-value") || !strings.Contains(string(raw), "test-os") {
		t.Fatal("invalid redacted diagnostics", readErr, closeErr)
	}
	replay, err := consumer.CreateDiagnosticsBundle(ctx, connect.NewRequest(request))
	if err != nil || !proto.Equal(replay.Msg.Operation, completed) || calls.Load() != 1 {
		t.Fatal("retry regenerated archive", err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "new-owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(&ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId}))
	// Administrator authorization does not restore a revoked archive handle.
	// Logical revocation must apply before the asynchronous physical purge.
	assertRPCFailure(t, err, rpcUnownedMissingResourceFailure(t, info))
	if err := s.purgeBundles(); err != nil {
		t.Fatal(err)
	}
	_, err = consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(&ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId}))
	assertRPCFailure(t, err, rpcUnownedMissingResourceFailure(t, info))
	restored, err := openClientRPCBundleStore(m.store.path+".bundles", m.now)
	if err != nil || len(restored.items) != 0 {
		t.Fatal("revoked artifact remained durable", err)
	}
}
