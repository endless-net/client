//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/clientapp"
)

func runPlatform(logger *slog.Logger, mode string, digest [32]byte) bool {
	environment := map[string]string{}
	if mode == "installed-run" || mode == "installed-init" {
		var err error
		environment, err = installedEnvironment()
		if err != nil {
			logger.Error("installed client configuration rejected")
			return false
		}
	} else {
		for _, entry := range os.Environ() {
			if key, value, ok := strings.Cut(entry, "="); ok {
				environment[key] = value
			}
		}
	}
	c, err := clientapp.ParseWindows(environment)
	if err != nil {
		logger.Error("client configuration rejected")
		return false
	}
	if mode == "init-state" || mode == "installed-init" {
		if clientapp.InitializeWindowsState(context.Background(), c) != nil {
			logger.Error("client state initialization rejected")
			return false
		}
		logger.Info("client empty state initialized")
		return true
	}
	if mode == "validate-config" {
		logger.Info("client configuration valid")
		return true
	}
	app, err := clientapp.NewWindows(c, &clientv0.BuildIdentity{Version: version, Commit: commit, BuildDate: buildDate, Platform: clientv0.Platform_PLATFORM_WINDOWS, Architecture: runtime.GOARCH}, digest, os.Stderr)
	if err != nil {
		logger.Error("client application rejected")
		return false
	}
	if app.RunSCM() != nil {
		logger.Error("client application stopped", "outcome", "failed")
		return false
	}
	return true
}
