// Package testclient drives a real client binary on disposable CI runners.
package testclient

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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
	ipc "github.com/endless-net/client/ipc/v2"
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
	n.AwaitStatus(func(s ipc.StatusResponse) bool { return s.IPCVersion == ipc.Version })
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
		<-n.done
	}
}

// ServiceCommand uses the same bounded native-operation timeout for success
// and expected error cases. Callers must not print arbitrary returned output.
func (n *Node) ServiceCommand(operation string, options ...string) ([]byte, error) {
	args := append([]string{"service", operation}, options...)
	return n.runWithin(35*time.Second, append(args, n.ipcArgs()...)...)
}

func (n *Node) Service(operation string, target any) {
	n.t.Helper()
	// Exercise the CLI's published default (30s), rather than imposing a 3s
	// mutation SLO that was never specified for native OS teardown operations.
	started := time.Now()
	out, err := n.ServiceCommand(operation)
	if err != nil {
		category := "unclassified"
		for _, known := range []string{"context deadline exceeded", "connection refused", "Access is denied", "The pipe is being closed"} {
			if strings.Contains(string(out), known) {
				category = known
				break
			}
		}
		// Never print arbitrary CLI output, errors, credentials or file content.
		n.t.Fatalf("client service %s failed after %s: %s (output withheld)", operation, time.Since(started).Round(time.Millisecond), category)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		n.t.Logf("client service %s completed after %s using the default CLI timeout", operation, elapsed.Round(time.Millisecond))
	}
	if err := json.Unmarshal(out, target); err != nil {
		n.t.Fatalf("invalid %s IPC JSON: %v", operation, err)
	}
}
func (n *Node) Status() (ipc.StatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, _, err := n.statusWithin(ctx)
	return status, err
}

// Only fixed error categories may leave the harness; arbitrary CLI output can
// contain configuration or credentials and must never be logged.
func (n *Node) statusWithin(ctx context.Context) (ipc.StatusResponse, string, error) {
	out, err := n.command(ctx, append([]string{"service", "status", "--timeout", "1s"}, n.ipcArgs()...)...).CombinedOutput()
	if err != nil {
		category := "unclassified"
		for _, known := range []string{"context deadline exceeded", "connection refused", "Access is denied", "The pipe is being closed", "The system cannot find the file specified"} {
			if strings.Contains(string(out), known) {
				category = known
				break
			}
		}
		return ipc.StatusResponse{}, category, err
	}
	var status ipc.StatusResponse
	err = json.Unmarshal(out, &status)
	if err != nil {
		return status, "invalid public JSON", err
	}
	return status, "", nil
}

func (n *Node) ipcArgs() []string {
	if runtime.GOOS == "windows" {
		return []string{"--ipc-pipe", n.Pipe}
	}
	return []string{"--ipc-socket", n.Socket}
}
func (n *Node) AwaitStatus(match func(ipc.StatusResponse) bool) ipc.StatusResponse {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var last ipc.StatusResponse
	responses, failures := 0, 0
	lastCategory := "none"
	err := Await(ctx, func() bool {
		status, category, err := n.statusWithin(ctx)
		if err == nil {
			responses++
			last = status
			return match(status)
		}
		failures++
		lastCategory = category
		return false
	})
	if err != nil {
		exited, exitCode := false, -1
		select {
		case processErr := <-n.done:
			exited = true
			if processErr == nil {
				exitCode = 0
			} else {
				var exit *exec.ExitError
				if errors.As(processErr, &exit) {
					exitCode = exit.ExitCode()
				}
			}
			n.done <- processErr // Preserve the process result for cleanup.
		default:
		}
		n.t.Fatalf("client state deadline: responses=%d failures=%d last_ipc_error=%q agent_exited=%t exit_code=%d state=%s control=%s revision=%d peers=%d", responses, failures, lastCategory, exited, exitCode, last.State, last.ControlState, last.MapRevision, last.PeerCount)
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
