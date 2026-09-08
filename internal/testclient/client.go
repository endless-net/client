// Package testclient drives a real client binary on disposable CI runners.
package testclient

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

type Node struct {
	t                                            *testing.T
	Binary, Config, Socket, Interface, TrustFile string
	cmd                                          *exec.Cmd
	done                                         chan error
}

func New(t *testing.T, s *testcontrol.Server) *Node {
	t.Helper()
	binary := os.Getenv("ENDLESSNET_TEST_BINARY")
	if !filepath.IsAbs(binary) {
		t.Fatal("ENDLESSNET_TEST_BINARY must be an absolute client binary path")
	}
	dir := t.TempDir()
	n := &Node{t: t, Binary: binary, Config: filepath.Join(dir, "client.json"), Socket: filepath.Join(dir, "ipc.sock"), Interface: "ent" + rand.Text()[:8], TrustFile: filepath.Join(dir, "trust.json")}
	// Linux Unix-domain socket paths are limited to 108 bytes.
	if len(n.Socket) >= 100 {
		short, err := os.MkdirTemp("", "ent-")
		if err != nil {
			t.Fatal(err)
		}
		n.Socket = filepath.Join(short, "ipc.sock")
		t.Cleanup(func() { _ = os.RemoveAll(short) })
	}
	data, err := json.Marshal(s.Trust())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(n.TrustFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Stop)
	return n
}

// Run keeps output in memory; failures never dump secrets from arbitrary CLI output.
func (n *Node) Run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.Binary, args...)
	return cmd.CombinedOutput()
}
func (n *Node) MustRun(args ...string) []byte {
	n.t.Helper()
	out, err := n.Run(args...)
	if err != nil {
		n.t.Fatalf("client %s failed: %v (output withheld)", args[0], err)
	}
	return out
}
func (n *Node) Enroll(s *testcontrol.Server, network, join string) {
	n.t.Helper()
	n.MustRun("up", "--config", n.Config, "--server", s.URL(), "--network", network, "--join-token", join, "--hostname", "scenario-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off")
}
func (n *Node) Start() {
	n.t.Helper()
	if n.cmd != nil {
		n.t.Fatal("agent already started")
	}
	n.cmd = exec.Command(n.Binary, "agent", "--config", n.Config, "--ipc-socket", n.Socket, "--wg-interface", n.Interface, "--interval", "100ms", "--timeout", "300ms", "--stun-timeout", "100ms", "--reconnect-max-delay", "300ms", "--reconnect-jitter", "0")
	n.cmd.Stdout = io.Discard
	n.cmd.Stderr = io.Discard
	if err := n.cmd.Start(); err != nil {
		n.cmd = nil
		n.t.Fatal(err)
	}
	n.done = make(chan error, 1)
	cmd := n.cmd
	done := n.done
	go func() { done <- cmd.Wait() }()
	n.AwaitStatus(func(s ipc.StatusResponse) bool { return s.IPCVersion == ipc.Version })
}
func (n *Node) Stop() {
	if n.cmd == nil {
		return
	}
	cmd := n.cmd
	n.cmd = nil
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-n.done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-n.done
	}
}
func (n *Node) Service(operation string, target any) {
	n.t.Helper()
	out := n.MustRun("service", operation, "--ipc-socket", n.Socket, "--timeout", "3s")
	if err := json.Unmarshal(out, target); err != nil {
		n.t.Fatalf("invalid %s IPC JSON: %v", operation, err)
	}
}
func (n *Node) Status() (ipc.StatusResponse, error) {
	out, err := n.Run("service", "status", "--ipc-socket", n.Socket, "--timeout", "1s")
	if err != nil {
		return ipc.StatusResponse{}, err
	}
	var status ipc.StatusResponse
	err = json.Unmarshal(out, &status)
	return status, err
}
func (n *Node) AwaitStatus(match func(ipc.StatusResponse) bool) ipc.StatusResponse {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var last ipc.StatusResponse
	err := Await(ctx, func() bool {
		status, err := n.Status()
		if err == nil {
			last = status
			return match(status)
		}
		return false
	})
	if err != nil {
		n.t.Fatalf("client state deadline: state=%s control=%s revision=%d peers=%d", last.State, last.ControlState, last.MapRevision, last.PeerCount)
	}
	return last
}
func Await(ctx context.Context, condition func() bool) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("condition not reached: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
