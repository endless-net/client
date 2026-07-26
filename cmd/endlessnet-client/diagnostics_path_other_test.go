//go:build !windows

package main

import (
	"os"
	"testing"
)

func prepareDiagnosticsTestDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("secure diagnostics test directory: %v", err)
	}
}
