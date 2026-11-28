package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/clientapp"
	"github.com/endless-net/client/internal/publicationproxy"
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
		var proxy *publicationproxy.LinuxRuntime
		if mode == "publication-proxy" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			var closeProxy func()
			var err error
			proxy, closeProxy, err = publicationproxy.OpenLinuxRuntime(ctx)
			cancel()
			if err != nil {
				logger.Error("publication proxy configuration unavailable")
				return false
			}
			defer closeProxy()
		}
		if clientapp.RunLinuxWithPublications(build, digest, os.Stderr, proxy) != nil {
			logger.Error("client application stopped", "outcome", "failed")
			return false
		}
	}
	return true
}
