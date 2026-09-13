//go:build windows || linux || darwin

package main

import (
	"context"
	"fmt"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestAgentNativeRPCHostBootstrapAndStop(t *testing.T) {
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-agent-rpc-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-host-")
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
	opts := agentIPCOptions{ConfigStore: store, OperationMu: &sync.Mutex{}, WireGuard: &testAgentWireGuard{}}
	if runtime.GOOS == "windows" {
		opts.Pipe = endpoint
	} else {
		opts.UnixSocket = endpoint
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	stop, err := startAgentRPC(ctx, cancel, opts)
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
	requestCtx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	if _, err := consumer.Bootstrap(requestCtx); err != nil {
		t.Fatal("agent did not expose native bootstrap", err)
	}
	transportFlag := "--ipc-socket"
	if runtime.GOOS == "windows" {
		transportFlag = "--ipc-pipe"
	}
	for command, response := range map[string]proto.Message{
		"status": &ipc.GetStatusResponse{}, "runtime-info": &ipc.GetRuntimeInfoResponse{}, "support-info": &ipc.GetSupportInfoResponse{},
	} {
		output, err := captureStdout(t, func() error {
			return cmdService([]string{command, transportFlag, endpoint, "--timeout", "5s"})
		})
		if err != nil {
			t.Fatal(command, err)
		}
		if err := protojson.Unmarshal([]byte(output), response); err != nil {
			t.Fatal("CLI response does not follow native protobuf JSON", command, err)
		}
	}
	eventOutput, err := captureStdout(t, func() error {
		return cmdService([]string{"events", transportFlag, endpoint, "--timeout", "1s"})
	})
	if err != nil {
		t.Fatal("native CLI event subscription failed", err)
	}
	event := new(ipc.WatchEventsResponse)
	if err := protojson.Unmarshal([]byte(eventOutput), event); err != nil || event.Sequence != 1 || event.GetSnapshot() == nil {
		t.Fatal("native CLI did not emit the opening snapshot", err)
	}
	requestID := "c96bfe40-876a-4bc8-95da-4fdd494ab48d"
	accepted, err := consumer.CreateProfile(requestCtx, connect.NewRequest(&ipc.CreateProfileRequest{
		Mutation:    &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: event.Metadata.InstanceId, ExpectedRevision: event.Metadata.Revision},
		DisplayName: "CLI recovery", ControlOrigin: "https://control.example.test",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for flag, id := range map[string]string{"--request-id": requestID, "--operation-id": accepted.Msg.Operation.Id} {
		output, err := captureStdout(t, func() error {
			return cmdService([]string{"operation", flag, id, transportFlag, endpoint, "--timeout", "5s"})
		})
		if err != nil {
			t.Fatal("CLI operation lookup failed", flag, err)
		}
		response := new(ipc.GetOperationResponse)
		if err := protojson.Unmarshal([]byte(output), response); err != nil || !proto.Equal(response.Operation, accepted.Msg.Operation) {
			t.Fatal("CLI did not recover the original operation", flag, err)
		}
	}
	output, err := captureStdout(t, func() error {
		return cmdService([]string{"operation", "--request-id", "3d68cf78-6998-42dc-9b62-aaae3dfc6535", transportFlag, endpoint, "--timeout", "5s"})
	})
	if connect.CodeOf(err) != connect.CodeNotFound || output != "" {
		t.Fatal("unknown request was not reported as missing", err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("normal host stop reported runtime failure", context.Cause(ctx))
	}
	if _, err := consumer.Bootstrap(requestCtx); err == nil {
		t.Fatal("native host continued accepting after stop")
	}
	consumer.Close()
	if err := store.Update(func(cfg *client.Config) error {
		cfg.RPCState = &client.ClientRPCState{DisconnectOperationID: "missing-operation"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	failedStop, err := startAgentRPC(ctx, cancel, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = failedStop() }()
	select {
	case <-ctx.Done():
		if err := failedStop(); err == nil || err != context.Cause(ctx) {
			t.Fatal("host failure did not reach the agent loop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed worker left the agent running")
	}
}

func TestAgentNativeRPCHostRejectsMixedEndpoints(t *testing.T) {
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = startAgentRPC(t.Context(), func(error) {}, agentIPCOptions{
		Pipe: "pipe", UnixSocket: "socket", ConfigStore: store, OperationMu: &sync.Mutex{}, WireGuard: &testAgentWireGuard{},
	})
	if err == nil {
		t.Fatal("mixed endpoints accepted")
	}
}
