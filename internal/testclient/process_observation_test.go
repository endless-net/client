package testclient

import (
	"errors"
	"testing"
)

func TestAgentCompletionObservationPreservesCleanupResult(t *testing.T) {
	for _, result := range []error{nil, errors.New("private process detail")} {
		done := make(chan error, 1)
		done <- result
		for range 2 {
			completed, failed := observeAgentCompletion(done)
			if !completed || failed != (result != nil) {
				t.Fatal("incorrect process completion classification")
			}
		}
		select {
		case got := <-done:
			if got != result {
				t.Fatal("observation replaced cleanup result")
			}
		default:
			t.Fatal("observation consumed cleanup result")
		}
	}
}

func TestAgentCompletionObservationDoesNotWait(t *testing.T) {
	for _, done := range []chan error{nil, make(chan error, 1)} {
		if completed, failed := observeAgentCompletion(done); completed || failed {
			t.Fatal("missing completion was treated as process exit")
		}
	}
}

func TestAgentExitCategoryIsAllowlisted(t *testing.T) {
	cases := map[string]string{
		"initialize logind lifecycle source: private bus error": "logind_initialization_failure",
		"close runtime after logind loss: private detail":       "logind_loss_enforcement_failure",
		"logind lifecycle source recovery exhausted":            "logind_recovery_exhausted",
		"logind sleep transition was not confirmed":             "logind_suspend_unconfirmed",
		"panic: private stack detail":                           "agent_panic",
		"fatal error: private runtime detail":                   "runtime_fatal_error",
		"unrecognized private error":                            "unclassified",
	}
	for input, want := range cases {
		if got := agentExitCategory([]byte(input)); got != want {
			t.Fatalf("agentExitCategory(%q) = %q, want %q", input, got, want)
		}
	}
	if got := agentExitCategory(nil); got != "no_stderr" {
		t.Fatalf("empty agent output category = %q", got)
	}
}

func TestBoundedProcessOutputRetainsOnlyLimit(t *testing.T) {
	output := &boundedProcessOutput{limit: 4}
	if n, err := output.Write([]byte("secret")); err != nil || n != len("secret") {
		t.Fatal("process output writer did not consume the complete write")
	}
	if got := string(output.snapshot()); got != "secr" {
		t.Fatalf("bounded output = %q, want capped prefix", got)
	}
}
