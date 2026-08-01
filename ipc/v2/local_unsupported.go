//go:build !windows && !linux && !darwin

package v2

import (
	"fmt"
	"runtime"
)

func NewLocalClient(string) (*Client, error) {
	return nil, fmt.Errorf("local EndlessNet IPC is unsupported on %s", runtime.GOOS)
}
