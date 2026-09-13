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

	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/internal/client"
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
