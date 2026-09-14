//go:build windows || linux || darwin

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeEnrollmentThroughRPCHostRetainsCredentialAuthority(t *testing.T) {
	setInstallationStateDirForTest(t, t.TempDir())
	backend, snapshot := testEnrollmentServer(t, testMapSigningKey(t), "synthetic-enrollment-token", 7)
	defer backend.Close()
	server := httptest.NewTLSServer(backend.Config.Handler)
	defer server.Close()
	// The producer clones DefaultTransport. Supply a TLS dialer which trusts
	// only this fixture certificate while retaining certificate verification.
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := tls.Dialer{Config: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != server.Listener.Addr().String() {
			return nil, fmt.Errorf("unexpected test TLS destination")
		}
		return dialer.DialContext(ctx, network, address)
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = originalTransport; transport.CloseIdleConnections() }()
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-enrollment-host-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-enroll-")
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
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &testAgentWireGuard{}
	opts := agentIPCOptions{ConfigStore: store, OperationMu: &sync.Mutex{}, WireGuard: engine}
	if runtime.GOOS == "windows" {
		opts.Pipe = endpoint
	} else {
		opts.UnixSocket = endpoint
	}
	deadline, end := context.WithTimeout(t.Context(), 10*time.Second)
	defer end()
	ctx, cancel := context.WithCancelCause(deadline)
	defer cancel(nil)
	stop, _, err := startAgentRPC(ctx, cancel, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if _, err := consumer.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	mutation := func(id string) *ipc.MutationContext {
		t.Helper()
		status, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		meta := status.Msg.Status.Metadata
		return &ipc.MutationContext{RequestId: id, ExpectedInstanceId: meta.InstanceId, ExpectedRevision: meta.Revision}
	}
	awaitOperation := func(id string) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			result, err := consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: id}}))
			if err != nil {
				t.Fatal(err)
			}
			if result.Msg.Operation.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				return
			}
			if result.Msg.Operation.State == ipc.OperationState_OPERATION_STATE_FAILED {
				t.Fatal("native worker failed", result.Msg.Operation.GetFailure().GetCode())
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal("native worker did not complete", context.Cause(ctx))
			}
		}
	}
	created, err := consumer.CreateProfile(ctx, connect.NewRequest(&ipc.CreateProfileRequest{Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889011"), DisplayName: "enrollment host", ControlOrigin: server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := consumer.SelectProfile(ctx, connect.NewRequest(&ipc.SelectProfileRequest{Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889013"), Profile: &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}}))
	if err != nil {
		t.Fatal(err)
	}
	awaitOperation(selected.Msg.Operation.Id)
	request := &ipc.EnrollRequest{Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889012"), Profile: &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}, Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE, Hostname: "native-host", Authentication: &ipc.EnrollRequest_EnrollmentToken{EnrollmentToken: "synthetic-enrollment-token"}}
	accepted, err := consumer.Enroll(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	awaitOperation(accepted.Msg.Operation.Id)
	replayed, err := consumer.Enroll(ctx, connect.NewRequest(request))
	if err != nil || replayed.Msg.Operation.Id != accepted.Msg.Operation.Id {
		t.Fatal("enrollment replay did not return original operation", err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	stored := store.Read()
	credential := agentRPCCredentialStatus(stored, time.Now())
	if stored.NodeCredentialSigningTrust == nil || credential.State != ipc.CredentialState_CREDENTIAL_STATE_VALID || credential.ExpiresAt == nil {
		t.Fatal("RPC enrollment lost verified credential authority")
	}
	registered, calls := snapshot()
	if calls != 1 || registered.IdempotencyID != accepted.Msg.Operation.Id || stored.NodeID != "node-1" || stored.CachedMap == nil || engine.configureCalls != 0 {
		t.Fatal("RPC enrollment repeated registration, lost map, or implicitly configured tunnel")
	}
}
