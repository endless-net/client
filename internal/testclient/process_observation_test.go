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
