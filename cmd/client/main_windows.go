//go:build windows

package main

import (
	"context"
	"encoding/hex"
	"fmt"
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
const clientAPIRevision = "b78b63d9ac9e3437207c358514a79792127616fa"
const clientContractSHA256 = "b57f96e0768571364ef88dc08f5b06375a52ac38eceabee9267a902ceb526817"

// Set only by the owner build helper from the selected clean commit/tag.
var version, commit, buildDate string

func main() {
	if len(os.Args) == 2 && os.Args[1] == "print-contract" {
		_, _ = fmt.Fprintln(os.Stdout, clientContractSHA256)
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "client", "component", "app", "client_api_revision", clientAPIRevision)
	mode := ""
	if len(os.Args) == 2 {
		mode = os.Args[1]
	}
	initialize := mode == "init-state" || mode == "installed-init"
	installed := mode == "installed-run" || mode == "installed-init"
	validate := mode == "validate-config"
	if len(os.Args) > 2 || (len(os.Args) > 1 && !initialize && !installed && !validate) {
		logger.Error("client command rejected")
		os.Exit(1)
	}
	environment := map[string]string{}
	if installed {
		var err error
		environment, err = installedEnvironment()
		if err != nil {
			logger.Error("installed client configuration rejected")
			os.Exit(1)
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
	if validate {
		logger.Info("client configuration valid")
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
