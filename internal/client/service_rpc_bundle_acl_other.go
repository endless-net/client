//go:build !windows

package client

import "os"

func isRPCBundleReparsePoint(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }

func diagnosticsFilePermissionsSecure(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0
}

func secureDiagnosticsBundleFile(path string) error { return os.Chmod(path, 0o600) }
