//go:build !windows

package client

import "os"

func diagnosticsFilePermissionsSecure(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0
}

func secureDiagnosticsBundleFile(path string) error { return os.Chmod(path, 0o600) }
