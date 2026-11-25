package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/clientapp"
)

func runPlatform(logger *slog.Logger, mode string, digest [32]byte) bool {
	switch mode {
	case "init-state", "installed-init":
		if clientapp.InitializeLinuxState(context.Background()) != nil {
			logger.Error("client state initialization rejected")
			return false
		}
		logger.Info("client state ready")
	case "validate-config":
		if clientapp.ValidateLinuxState(context.Background()) != nil {
			logger.Error("client state unavailable")
			return false
		}
		logger.Info("client state ready")
	default:
		build := &clientv0.BuildIdentity{Version: version, Commit: commit, BuildDate: buildDate, Platform: clientv0.Platform_PLATFORM_LINUX, Architecture: runtime.GOARCH}
		if clientapp.RunLinux(build, digest, os.Stderr) != nil {
			logger.Error("client application stopped", "outcome", "failed")
			return false
		}
	}
	return true
}
