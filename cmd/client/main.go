package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"
)

// Packaging verifies these opaque pins against the resolved immutable SDK.
const clientAPIRevision = "a50e997641466d504a216083cf7ddd9aeab6a755"
const clientContractSHA256 = "6ecbb4474e7b5734d0561e2d6861fc05a0681d193667cb9449859e4a8edf873f"

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
	if len(os.Args) > 2 || (len(os.Args) == 2 && mode != "init-state" && mode != "installed-init" && mode != "installed-run" && mode != "validate-config") {
		logger.Error("client command rejected")
		os.Exit(1)
	}
	date, dateErr := time.Parse(time.RFC3339, buildDate)
	digest, digestErr := hex.DecodeString(clientContractSHA256)
	if !regexp.MustCompile(`^(v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)|[0-9a-f]{12})$`).MatchString(version) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) || dateErr != nil || date.IsZero() || digestErr != nil || len(digest) != 32 {
		logger.Error("client build identity rejected")
		os.Exit(1)
	}
	if !runPlatform(logger, mode, [32]byte(digest)) {
		os.Exit(1)
	}
}
