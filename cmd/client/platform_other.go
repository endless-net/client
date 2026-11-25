//go:build !windows && !linux

package main

import (
	"log/slog"
	"runtime"
)

func runPlatform(logger *slog.Logger, _ string, _ [32]byte) bool {
	logger.Error("client runtime is unavailable on this platform", "platform", runtime.GOOS)
	return false
}
