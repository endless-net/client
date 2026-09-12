package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrAgentAlreadyRunning = errors.New("agent already running for this config")

type AgentLock struct {
	file *os.File
}

func AgentLockPath(configPath string) (string, error) {
	resolved, err := resolveConfigPath(configPath)
	if err != nil {
		return "", err
	}
	return resolved + ".agent.lock", nil
}

func AcquireAgentLock(path string) (*AgentLock, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("agent lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryLockFile(file); err != nil {
		_ = file.Close()
		if isLockHeldError(err) {
			return nil, fmt.Errorf("%w: %s", ErrAgentAlreadyRunning, path)
		}
		return nil, err
	}
	_ = file.Truncate(0)
	_, _ = fmt.Fprintf(file, "pid=%d\n", os.Getpid())
	_ = file.Sync()
	return &AgentLock{file: file}, nil
}

func (l *AgentLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
