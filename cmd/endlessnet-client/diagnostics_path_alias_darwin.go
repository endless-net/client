//go:build darwin

package main

import (
	"os"
	"path/filepath"
)

func isTrustedDiagnosticsPathAlias(path string, info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	want, ok := map[string]string{
		"/tmp": "/private/tmp",
		"/var": "/private/var",
	}[filepath.Clean(path)]
	if !ok {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && filepath.Clean(resolved) == want
}
