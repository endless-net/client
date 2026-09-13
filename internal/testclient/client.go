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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testcontrol"
)

type Node struct {
	t                                            *testing.T
	Binary, Config, Socket, Interface, TrustFile string
	Namespace, Pipe                              string
	AgentArgs                                    []string
	Environment                                  []string
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
	if runtime.GOOS == "windows" {
		n.Pipe = `\\.\pipe\endlessnet-test-` + rand.Text()
	}
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
	if public := s.TLSCertificatePEM(); len(public) > 0 {
		path := filepath.Join(dir, "test-tls-ca.pem")
		if err := os.WriteFile(path, public, 0o600); err != nil {
			t.Fatal(err)
		}
		n.Environment = []string{"SSL_CERT_FILE=" + path}
	}
	t.Cleanup(n.Stop)
	return n
}

// Run keeps output in memory; failures never dump secrets from arbitrary CLI output.
func (n *Node) Run(args ...string) ([]byte, error) {
	return n.runWithin(15*time.Second, args...)
}

// RunContext allows a scenario to interrupt a real CLI process at an observed
// wire boundary. The usual harness deadline still bounds the invocation.
func (n *Node) RunContext(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return n.command(ctx, args...).CombinedOutput()
}

func (n *Node) runWithin(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := n.command(ctx, args...)
	return cmd.CombinedOutput()
}

func (n *Node) command(ctx context.Context, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if n.Namespace != "" {
		cmd = exec.CommandContext(ctx, "ip", append([]string{"netns", "exec", n.Namespace, n.Binary}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, n.Binary, args...)
	}
	cmd.Env = append(os.Environ(), n.Environment...)
	// A descendant may retain a copied output pipe after the Client exits.
	// Context cancellation alone does not bound os/exec's pipe-copy wait.
	cmd.WaitDelay = 2 * time.Second
	return cmd
}
func (n *Node) MustRun(args ...string) []byte {
	n.t.Helper()
	out, err := n.Run(args...)
	if err != nil {
		n.t.Fatalf("client %s failed: %v (output withheld)", args[0], err)
	}
	return out
}
func (n *Node) Enroll(s *testcontrol.Server, network, join string, options ...string) {
	n.t.Helper()
	args := []string{"up", "--config", n.Config, "--server", s.URL(), "--network", network, "--join-token", join, "--hostname", "scenario-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off"}
	n.MustRun(append(args, options...)...)
}
func (n *Node) Start() {
	n.t.Helper()
	if n.cmd != nil {
		n.t.Fatal("agent already started")
	}
	args := []string{"agent", "--config", n.Config, "--state-output", filepath.Join(filepath.Dir(n.Config), "agent-state.json"), "--wg-interface", n.Interface, "--interval", "100ms", "--timeout", "300ms", "--stun-timeout", "100ms", "--reconnect-max-delay", "300ms", "--reconnect-jitter", "0"}
	args = append(args, n.ipcArgs()...)
	n.cmd = n.command(context.Background(), append(args, n.AgentArgs...)...)
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
	// Match the public service IPC default and installed-service startup wait.
	// Native driver initialization can outlast the shorter state-transition wait.
	started := time.Now()
	n.awaitNativeReady()
	n.t.Logf("agent IPC became ready after %s", time.Since(started).Round(time.Millisecond))
}

// Crash terminates the real process without a graceful shutdown signal.
func (n *Node) Crash() {
	n.t.Helper()
	if n.cmd == nil {
		n.t.Fatal("no running agent to terminate")
	}
	cmd := n.cmd
	if err := cmd.Process.Kill(); err != nil {
		n.t.Fatal("could not terminate the agent")
	}
	n.cmd = nil
	select {
	case err := <-n.done:
		if err == nil {
			n.t.Fatal("forced agent termination unexpectedly returned success")
		}
	case <-time.After(5 * time.Second):
		n.t.Fatal("forced agent termination did not complete")
	}
}

func (n *Node) Stop() {
	if n.cmd == nil {
		return
	}
	cmd := n.cmd
	n.cmd = nil
	// Go agents handle Interrupt on Unix. Windows process termination is abrupt;
	// service-manager restart semantics are covered by the installation suite.
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-n.done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		n.awaitKilledProcess()
	}
}

func (n *Node) awaitKilledProcess() {
	n.t.Helper()
	select {
	case <-n.done:
	case <-time.After(5 * time.Second):
		n.t.Error("agent process wait did not complete after forced termination")
	}
}

// StopWithSignal verifies clean foreground shutdown rather than accepting a
// fallback process kill. Callers choose only signals supported by the platform.
func (n *Node) StopWithSignal(signal os.Signal) {
	n.t.Helper()
	if n.cmd == nil {
		n.t.Fatal("no agent process to signal")
	}
	cmd := n.cmd
	n.cmd = nil
	if err := cmd.Process.Signal(signal); err != nil {
		_ = cmd.Process.Kill()
		n.awaitKilledProcess()
		n.t.Fatal("could not deliver the foreground termination signal")
	}
	select {
	case err := <-n.done:
		if err != nil {
			n.t.Fatal("foreground signal shutdown did not exit successfully")
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		n.awaitKilledProcess()
		n.t.Fatal("foreground signal shutdown required a forced kill")
	}
}

// ServiceCommand uses the same bounded native-operation timeout for success
// and expected error cases. Callers must not print arbitrary returned output.
func (n *Node) ServiceCommand(operation string, options ...string) ([]byte, error) {
	args := append([]string{"service", operation}, options...)
	return n.runWithin(35*time.Second, append(args, n.ipcArgs()...)...)
}

func (n *Node) ipcArgs() []string {
	if runtime.GOOS == "windows" {
		return []string{"--ipc-pipe", n.Pipe}
	}
	return []string{"--ipc-socket", n.Socket}
}

// Collect only fixed public log messages after a failed wait. This separate
// bounded request does not extend the readiness deadline or inspect agent files.
func (n *Node) logWireGuardStartupStages() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stages, err := nativeStartupStages(ctx, func(ctx context.Context, operation string, options ...string) ([]byte, error) {
		args := append([]string{"service", operation, "--timeout", "1s"}, n.ipcArgs()...)
		return n.command(ctx, append(args, options...)...).CombinedOutput()
	})
	if err != nil {
		n.t.Log("public startup-stage log unavailable")
		return
	}
	n.t.Logf("public WireGuard startup stages: %v", stages)
}

func wireGuardStartupStage(message string) string {
	switch message {
	case "WireGuard engine: tun-create begin", "WireGuard engine: tun-create complete",
		"WireGuard engine: device-up begin", "WireGuard engine: device-up complete",
		"WireGuard engine: routes begin", "WireGuard engine: routes complete":
		return strings.TrimPrefix(message, "WireGuard engine: ")
	default:
		return ""
	}
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
