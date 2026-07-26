package client

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireAgentLockRejectsConcurrentHolder(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "client.json")
	lockPath, err := AgentLockPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := AcquireAgentLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second, err := AcquireAgentLock(lockPath)
	if err == nil {
		_ = second.Close()
		t.Fatal("second agent lock unexpectedly succeeded")
	}
	if !errors.Is(err, ErrAgentAlreadyRunning) {
		t.Fatalf("second lock error = %v, want ErrAgentAlreadyRunning", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := AcquireAgentLock(lockPath)
	if err != nil {
		t.Fatalf("lock after release failed: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}
