//go:build windows

package main

import (
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/clientapp"
)

// Packaging verifies these opaque pins against the resolved immutable SDK.
const clientAPIRevision = "0600e7a17d3bc8ad4c2c844109b4abbbeb7ae0a9"
const clientContractSHA256 = "cf89bccd1774dbb4d720945832be302fe7d15be886892b48d218bc1cabbfebd3"

// Set only by the owner build helper from the selected clean commit/tag.
var version, commit, buildDate string

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "client", "component", "app", "client_api_revision", clientAPIRevision)
	initialize := len(os.Args) == 2 && os.Args[1] == "init-state"
	if len(os.Args) > 1 && !initialize {
		logger.Error("client command rejected")
		os.Exit(1)
	}
	environment := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			environment[key] = value
		}
	}
	c, err := clientapp.ParseWindows(environment)
	if err != nil {
		logger.Error("client configuration rejected")
		os.Exit(1)
	}
	date, dateErr := time.Parse(time.RFC3339, buildDate)
	digest, digestErr := hex.DecodeString(clientContractSHA256)
	if !regexp.MustCompile(`^(v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)|[0-9a-f]{12})$`).MatchString(version) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) || dateErr != nil || date.IsZero() || digestErr != nil || len(digest) != 32 {
		logger.Error("client build identity rejected")
		os.Exit(1)
	}
	if initialize {
		if clientapp.InitializeWindowsState(context.Background(), c) != nil {
			logger.Error("client state initialization rejected")
			os.Exit(1)
		}
		logger.Info("client empty state initialized")
		return
	}
	app, err := clientapp.NewWindows(c, &clientv0.BuildIdentity{Version: version, Commit: commit, BuildDate: buildDate, Platform: clientv0.Platform_PLATFORM_WINDOWS, Architecture: runtime.GOARCH}, [32]byte(digest), os.Stderr)
	if err != nil {
		logger.Error("client application rejected")
		os.Exit(1)
	}
	if app.RunSCM() != nil {
		logger.Error("client application stopped", "outcome", "failed")
		os.Exit(1)
	}
}
