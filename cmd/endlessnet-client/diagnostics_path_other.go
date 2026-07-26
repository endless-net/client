//go:build !windows

package main

import "os"

func isDiagnosticsReparsePoint(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}

func diagnosticsDirectoryPermissionsSecure(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode().Perm()&0o077 == 0
}

func diagnosticsFilePermissionsSecure(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode().Perm()&0o077 == 0
}

func secureNewDiagnosticsDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

func secureDiagnosticsBundleFile(path string) error {
	return os.Chmod(path, 0o600)
}
