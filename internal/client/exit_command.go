package client

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var errExitCommandOutputLimit = errors.New("exit command output exceeds limit")

// Stdout and stderr share one writer; os/exec serializes writes to it. Cancel
// the command on overflow, retaining no more than limit bytes for observation.
type exitCommandOutput struct {
	data     []byte
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (o *exitCommandOutput) Write(p []byte) (int, error) {
	if o.overflow {
		return 0, errExitCommandOutputLimit
	}
	if len(p) > o.limit-len(o.data) {
		o.overflow = true
		o.cancel()
		return 0, errExitCommandOutputLimit
	}
	o.data = append(o.data, p...)
	return len(p), nil
}

func runExitCommand(ctx context.Context, input, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &exitCommandOutput{limit: 1 << 20, cancel: cancel}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout, cmd.Stderr = output, output
	// Bound inherited pipe lifetime even if a command's child keeps it open.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if output.overflow {
		return nil, errExitCommandOutputLimit
	}
	if err != nil {
		return nil, err
	}
	return output.data, nil
}
