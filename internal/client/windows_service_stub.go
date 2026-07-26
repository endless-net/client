//go:build !windows

package client

import (
	"context"
	"errors"
)

func RunWindowsService(name string, run func(context.Context) error) error {
	return errors.New("windows service mode is only supported on Windows")
}
