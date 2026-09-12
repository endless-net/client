//go:build windows || linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	pb "github.com/endless-net/client/clientipc/v0"
)

func TestHostServesLocalScriptAndVerifies(t *testing.T) {
	dir := t.TempDir()
	endpoint := filepath.Join(dir, "test.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\endlessnet-script-%d`, time.Now().UnixNano())
	} else {
		shortDir, err := os.MkdirTemp("/tmp", "en-script-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(shortDir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(shortDir, "rpc.sock")
	}
	script := filepath.Join(dir, "scenario.json")
	if err := os.WriteFile(script, []byte(`{"steps":[{"method":"GetStatus","request":{},"responses":[{"status":{"connectionPhase":"CONNECTION_PHASE_CONNECTING"}}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	inputR, inputW := io.Pipe()
	outputR, outputW := io.Pipe()
	t.Cleanup(func() { _ = inputW.Close(); _ = outputR.Close() })
	done := make(chan error, 1)
	go func() {
		defer func() { _ = outputW.Close() }()
		defer func() { _ = inputR.Close() }()
		done <- run([]string{"--script", script, "--endpoint", endpoint}, inputR, outputW)
	}()
	decoder := json.NewDecoder(outputR)
	var event map[string]string
	if err := decoder.Decode(&event); err != nil || event["event"] != "ready" || event["contract_sha256"] != rpc.Digest() {
		t.Fatal("missing readiness")
	}
	client, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// Role simulation is configured by the host, never supplied by the consumer.
	_, err = client.Connect(ctx, connect.NewRequest(&pb.ConnectRequest{}))
	if rpc.FailureFromError(err).GetCode() != pb.ErrorCode_ERROR_CODE_OWNER_REQUIRED {
		t.Fatal("observer could mutate")
	}
	response, err := client.GetStatus(ctx, connect.NewRequest(&pb.GetStatusRequest{}))
	if err != nil || response.Msg.GetStatus().GetConnectionPhase() != pb.ConnectionPhase_CONNECTION_PHASE_CONNECTING {
		t.Fatalf("script RPC failed: %v", err)
	}
	client.Close()
	if _, err := io.WriteString(inputW, "verify\n"); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&event); err != nil || event["event"] != "verified" {
		t.Fatal("missing verified outcome")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("host did not terminate")
	}
}

func TestHostRejectsProductionEndpoints(t *testing.T) {
	for _, endpoint := range []string{local.DefaultWindowsPipe, local.DefaultUnixSocket, local.DefaultDarwinSocket} {
		if err := run([]string{"--script", "unused", "--endpoint", endpoint}, bytes.NewReader(nil), io.Discard); err == nil {
			t.Fatal("production endpoint accepted")
		}
	}
}
