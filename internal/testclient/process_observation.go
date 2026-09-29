package testclient

import (
	"strings"
	"sync"
)

type boundedProcessOutput struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *boundedProcessOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return written, nil
	}
	if excess := len(b.data) + len(p) - b.limit; excess > 0 {
		copy(b.data, b.data[excess:])
		b.data = b.data[:len(b.data)-excess]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *boundedProcessOutput) snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

// agentExitCategory maps only known top-level termination messages to fixed
// labels. It never returns or logs captured process output.
func agentExitCategory(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "initialize logind lifecycle source:"):
			return "logind_initialization_failure"
		case strings.HasPrefix(line, "close runtime after logind loss:"):
			return "logind_loss_enforcement_failure"
		case line == "logind lifecycle source recovery exhausted":
			return "logind_recovery_exhausted"
		case line == "logind sleep transition was not confirmed":
			return "logind_suspend_unconfirmed"
		case strings.HasPrefix(line, "panic:"):
			return "agent_panic"
		case strings.HasPrefix(line, "fatal error:"):
			return "runtime_fatal_error"
		}
	}
	if len(output) == 0 {
		return "no_stderr"
	}
	return "unclassified"
}

func logindLifecycleStages(output []byte) []string {
	var stages []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "logind lifecycle stage: ") {
			continue
		}
		switch line {
		case "logind lifecycle stage: reconnect open failed",
			"logind lifecycle stage: reconnect open succeeded",
			"logind lifecycle stage: missed logoff delivery failed",
			"logind lifecycle stage: preparing state read stopped",
			"logind lifecycle stage: preparing state D-Bus read failed",
			"logind lifecycle stage: preparing state value invalid",
			"logind lifecycle stage: preparing state value nested variant",
			"logind lifecycle stage: preparing state value string",
			"logind lifecycle stage: preparing state value uint32",
			"logind lifecycle stage: preparing state value uint64",
			"logind lifecycle stage: preparing state value int32",
			"logind lifecycle stage: preparing state value int64",
			"logind lifecycle stage: resume notification stopped",
			"logind lifecycle stage: suspend notification stopped",
			"logind lifecycle stage: signal listener stopped":
			stages = append(stages, line)
		}
	}
	return stages
}

// observeAgentCompletion is used only by the synchronous readiness failure path.
// The buffered completion remains available to Stop/Crash; the observation never
// waits for the process, reads its output, or replaces the readiness predicate.
func observeAgentCompletion(done chan error) (completed, failed bool) {
	select {
	case err := <-done:
		done <- err
		return true, err != nil
	default:
		return false, false
	}
}
